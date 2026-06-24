// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package notification

import (
	"context"
	"encoding/json"
	"fmt"

	"nimblegate/internal/gateway/upstream"
	"nimblegate/internal/gateway/webhook"
)

// Orchestrator wires the per-notification delivery flow: find PR → read
// PR people → render markdown (which embeds the JSON block) → upsert sticky
// comment → always fire webhook when configured.
//
// Pre-receive uses Orchestrator.DeliverOne for the opportunistic inline
// attempt (Task 22). The daemon uses it for the background drain (Task 23).
// Same logic, two callers, no duplication.
type Orchestrator struct {
	Upstreams  *upstream.Registry
	Webhook    *webhook.Client
	Render     func(Notification) string // typically render.Comment
	PolicyRoot string                    // for PRState persistence
}

// DeliverOne runs the full delivery flow for a single queue record.
// Returns nil on success (caller removes the queue record). Returns an
// error wrapping upstream.ErrTransient or ErrPermanent so the daemon can
// classify it for retry vs deadletter routing.
func (o *Orchestrator) DeliverOne(ctx context.Context, rec QueueRecord) error {
	adapter, err := o.Upstreams.LookupByURL(rec.Notification.Repo.UpstreamURL)
	if err != nil {
		return fmt.Errorf("%w: %v", upstream.ErrPermanent, err)
	}

	// 1. Find PR - no-PR case: skip comment work, fire webhook only
	if len(rec.Notification.Push.Refs) == 0 {
		return fmt.Errorf("%w: queue record has no push refs", upstream.ErrPermanent)
	}
	pr, err := adapter.FindPRForRef(ctx, rec.Notification.Repo.Name, rec.Notification.Push.Refs[0].Name)
	if err != nil {
		return fmt.Errorf("find PR: %w", err)
	}

	// Resolution: a clean push closed the loop. Update the sticky comment to a
	// ✅ resolved state and stop - no people read, no loop transition. The PR
	// state was already cleared by pre-receive; the sticky id rides on the
	// queue record. If there's no PR or no sticky to update, there's nothing to
	// resolve and the webhook (below) still fires.
	if rec.Notification.Event == "push.resolved" {
		if pr != nil {
			rec.Notification.Push.PR = &PRInfo{Number: pr.Number, URL: pr.URL}
			var sticky *upstream.Comment
			if rec.State.StickyCommentID != "" {
				sticky, _ = adapter.FindStickyComment(ctx, pr, rec.State.StickyCommentID)
			}
			if sticky == nil {
				sticky, _ = adapter.ScanForMarker(ctx, pr, "<!-- nimblegate-data:")
			}
			if sticky != nil {
				if err := adapter.UpdateComment(ctx, sticky, o.Render(rec.Notification)); err != nil {
					return fmt.Errorf("update resolved comment: %w", err)
				}
			}
		}
	} else if pr != nil {
		repoName := rec.Notification.Repo.Name

		// 2. Read assignees + reviewers
		people, err := adapter.ReadPRPeople(ctx, pr)
		if err != nil {
			return fmt.Errorf("read PR people: %w", err)
		}
		// Augment notification with live PR info (assignees, mention)
		rec.Notification.Push.PR = &PRInfo{
			Number:    pr.Number,
			URL:       pr.URL,
			Assignees: people.Assignees,
			Reviewers: people.Reviewers,
		}

		// 2b. Advance the per-PR loop state (attempt count + bot rotation) now
		// that the PR number is known - only when a real loop config rode along
		// on the queue record. Reflect it into the notification so the comment
		// shows "attempt N/M" and @-mentions the current bot. Persisted once
		// below (with any new sticky id). With no loop config (e.g. unit tests
		// driving DeliverOne directly), the bare path runs exactly as before.
		var loopState PRState
		trackLoop := rec.LoopConfig.MaxAttempts > 0
		if trackLoop {
			prev, _ := ReadPRState(o.PolicyRoot, repoName, pr.Number)
			loopState = Transition(prev, RejectEvent{
				Findings: rec.Notification.Decision.Findings,
				PushSHA:  firstNewSHA(rec.Notification.Push.Refs),
			}, rec.LoopConfig)
			loopState.PRNumber = pr.Number
			loopState.Repo = repoName
			if len(rec.Notification.Push.Refs) > 0 {
				loopState.Ref = rec.Notification.Push.Refs[0].Name
			}
			if prev != nil {
				loopState.StickyComment = prev.StickyComment // keep sticky ref across attempts
			}
			applyLoopState(&rec.Notification, loopState, people)
		} else {
			if rec.Notification.Mention == nil {
				rec.Notification.Mention = &MentionInfo{}
			}
			rec.Notification.Mention.AutoTaggedHumans = dedupeHumans(people, rec.Notification.Mention.CurrentBot)
		}

		// 3. Sticky lookup - try by ID first, fall back to marker scan
		stickyID := rec.State.StickyCommentID
		if trackLoop {
			stickyID = loopState.StickyComment.ID
		}
		var sticky *upstream.Comment
		if stickyID != "" {
			sticky, _ = adapter.FindStickyComment(ctx, pr, stickyID)
		}
		if sticky == nil {
			sticky, _ = adapter.ScanForMarker(ctx, pr, "<!-- nimblegate-data:")
		}

		// 4. Render markdown body
		body := o.Render(rec.Notification)

		// 5. Upsert (update if sticky found, create otherwise)
		if sticky != nil {
			if err := adapter.UpdateComment(ctx, sticky, body); err != nil {
				return fmt.Errorf("update comment: %w", err)
			}
		} else {
			newComment, err := adapter.CreateComment(ctx, pr, body)
			if err != nil {
				return fmt.Errorf("create comment: %w", err)
			}
			if trackLoop {
				loopState.StickyComment.ID = newComment.ID
				loopState.StickyComment.URL = newComment.URL
				loopState.StickyComment.LastUpdatedAt = newComment.CreatedAt
			} else {
				// Legacy path: persist sticky ID into the existing PRState.
				s, _ := ReadPRState(o.PolicyRoot, repoName, pr.Number)
				if s != nil {
					s.StickyComment.ID = newComment.ID
					s.StickyComment.URL = newComment.URL
					s.StickyComment.LastUpdatedAt = newComment.CreatedAt
					_ = WritePRState(o.PolicyRoot, repoName, pr.Number, *s)
				}
			}
		}

		// 6. Persist the advanced loop state (incl. any new sticky id) once.
		if trackLoop {
			_ = WritePRState(o.PolicyRoot, repoName, pr.Number, loopState)
		}
	}

	// 6. Webhook ALWAYS fires when configured, regardless of PR comment outcome
	if rec.WebhookURL != "" {
		payload, err := json.Marshal(rec.Notification)
		if err != nil {
			return fmt.Errorf("%w: marshal notification: %v", upstream.ErrPermanent, err)
		}
		auth := webhook.Auth{Mode: rec.WebhookAuth.Mode, Secret: rec.WebhookAuth.Secret, HeaderName: rec.WebhookAuth.HeaderName}
		if err := o.Webhook.Deliver(ctx, rec.WebhookURL, payload, auth); err != nil {
			return fmt.Errorf("webhook: %w", err)
		}
	}
	return nil
}

// applyLoopState reflects the persisted PR loop state into the notification so
// the rendered comment shows attempt N/M and @-mentions the current bot.
func applyLoopState(n *Notification, s PRState, people upstream.PRPeople) {
	n.LoopState = &LoopState{
		PRAttemptCount:   s.Loop.AttemptCount,
		MaxAttempts:      s.Loop.MaxAttempts,
		PreviousAttempts: priorAttempts(s.AttemptHistory),
	}
	if n.Mention == nil {
		n.Mention = &MentionInfo{}
	}
	n.Mention.CurrentBot = s.Mention.CurrentBot
	n.Mention.FallbackActive = s.Mention.FallbackActive
	n.Mention.AutoTaggedHumans = dedupeHumans(people, s.Mention.CurrentBot)

	// Rotation banner: when the latest attempt rotated to a new bot, surface
	// the "🔄 Rotated from … " line. Transition records the rotation on the last
	// history entry (RotatedAfter + RotationReason); the prior entry's Bot is
	// the bot we rotated away from.
	if h := s.AttemptHistory; len(h) > 0 && h[len(h)-1].RotatedAfter {
		from := ""
		if len(h) >= 2 {
			from = h[len(h)-2].Bot
		}
		n.Mention.Rotation = &RotationInfo{
			Enabled:       true,
			AttemptIndex:  s.Loop.AttemptCount,
			RotatedFrom:   from,
			RotatedReason: h[len(h)-1].RotationReason,
		}
	}
}

// priorAttempts projects the persisted attempt history to the wire shape,
// excluding the current (last) attempt - the comment lists PRIOR attempts.
func priorAttempts(h []HistoryEntry) []AttemptRecord {
	if len(h) <= 1 {
		return nil
	}
	out := make([]AttemptRecord, 0, len(h)-1)
	for _, e := range h[:len(h)-1] {
		out = append(out, AttemptRecord{SHA: e.SHA, Timestamp: e.Timestamp, Bot: e.Bot})
	}
	return out
}

// firstNewSHA returns the new SHA of the first ref update, for loop history.
func firstNewSHA(refs []RefInfo) string {
	if len(refs) > 0 {
		return refs[0].NewSHA
	}
	return ""
}

// dedupeHumans returns @-prefixed handles from people.Assignees +
// people.Reviewers, skipping anything equal to the currentBot mention.
func dedupeHumans(people upstream.PRPeople, currentBot string) []string {
	seen := map[string]bool{currentBot: true}
	out := []string{}
	for _, h := range append(people.Assignees, people.Reviewers...) {
		handle := "@" + h
		if !seen[handle] {
			seen[handle] = true
			out = append(out, handle)
		}
	}
	return out
}
