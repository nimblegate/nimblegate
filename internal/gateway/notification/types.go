// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

// Package notification implements the auto-PR + webhook rail per
// docs/superpowers/specs/2026-06-04-auto-pr-and-webhook-design.md.
package notification

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// SchemaVersion is the wire-format version emitted in every Notification.
const SchemaVersion = "1.0"

// Notification is the payload sent to both channels (hidden HTML block in PR
// comment + webhook POST body). Versioned via SchemaVersion; consumers compare
// the field to their expected major.minor.
type Notification struct {
	SchemaVersion string       `json:"schema_version"`
	Event         string       `json:"event"`    // "push.rejected" | "push.observed"
	EventID       string       `json:"event_id"` // unique per notification; webhook idempotency key
	Gateway       GatewayInfo  `json:"gateway"`
	Repo          RepoInfo     `json:"repo"`
	Push          PushInfo     `json:"push"`
	Decision      DecisionInfo `json:"decision"`
	Mention       *MentionInfo `json:"mention,omitempty"`
	LoopState     *LoopState   `json:"loop_state,omitempty"`
	NextSteps     *NextSteps   `json:"next_steps,omitempty"`
}

type GatewayInfo struct {
	Name       string `json:"name"`
	Version    string `json:"version"`
	InstanceID string `json:"instance_id"`
}

type RepoInfo struct {
	Name        string `json:"name"`
	UpstreamURL string `json:"upstream_url"`
}

type PushInfo struct {
	Timestamp            time.Time `json:"timestamp"`
	PusherKeyFingerprint string    `json:"pusher_key_fingerprint,omitempty"`
	Refs                 []RefInfo `json:"refs"`
	PR                   *PRInfo   `json:"pr,omitempty"` // nil when no PR open on the ref
}

type RefInfo struct {
	Name   string `json:"name"` // refs/heads/main
	OldSHA string `json:"old_sha"`
	NewSHA string `json:"new_sha"`
	Type   string `json:"type"` // "create" | "update" | "delete"
}

type PRInfo struct {
	Number    int      `json:"number"`
	URL       string   `json:"url"`
	Assignees []string `json:"assignees,omitempty"` // login handles, no @ prefix
	Reviewers []string `json:"reviewers,omitempty"`
}

type DecisionInfo struct {
	Accepted   bool                `json:"accepted"`
	Observed   bool                `json:"observed,omitempty"`
	Findings   []Finding           `json:"findings,omitempty"`
	Suppressed []SuppressedFinding `json:"suppressed,omitempty"`
}

// Finding is the notification-layer view of a check result - richer than
// gateway.Finding because the comment needs file/line/hint per finding, plus
// the stable fingerprint that powers same-finding-twice rotation.
type Finding struct {
	FrameID     string `json:"frame_id"`
	Severity    string `json:"severity"` // BLOCK | ERROR | WARN | INFO (matches gateway.Finding)
	Message     string `json:"message"`
	File        string `json:"file,omitempty"`
	Line        int    `json:"line,omitempty"`
	Hint        string `json:"hint,omitempty"`
	Fingerprint string `json:"fingerprint"` // sha256(frame_id + file + line)
}

type SuppressedFinding struct {
	FrameID string `json:"frame_id"`
	File    string `json:"file"`
	Reason  string `json:"reason"`
}

type MentionInfo struct {
	Default          string        `json:"default,omitempty"`
	CurrentBot       string        `json:"current_bot,omitempty"`
	Rotation         *RotationInfo `json:"rotation,omitempty"`
	AutoTaggedHumans []string      `json:"auto_tagged_humans,omitempty"`
	FallbackActive   bool          `json:"fallback_active,omitempty"`
}

type RotationInfo struct {
	Enabled       bool   `json:"enabled"`
	AttemptIndex  int    `json:"attempt_index"`
	RotatedFrom   string `json:"rotated_from,omitempty"`
	RotatedReason string `json:"rotated_reason,omitempty"` // "attempt-threshold" | "same-finding" | "exhaustion"
}

type LoopState struct {
	PRAttemptCount        int             `json:"pr_attempt_count"`
	MaxAttempts           int             `json:"max_attempts"`
	SameFindingAsPrevious bool            `json:"same_finding_as_previous,omitempty"`
	PreviousAttempts      []AttemptRecord `json:"previous_attempts,omitempty"`
}

type AttemptRecord struct {
	SHA       string    `json:"sha"`
	Timestamp time.Time `json:"timestamp"`
	Bot       string    `json:"bot,omitempty"`
}

type NextSteps struct {
	FixAndRepush   string `json:"fix_and_repush,omitempty"`
	RequestRecheck string `json:"request_recheck,omitempty"`
	ViewFullAudit  string `json:"view_full_audit,omitempty"`
}

// BuildInput is the gateway-agnostic projection of the decision context that
// Build needs to construct a Notification. The gateway package converts its
// internal types (Policy, RefUpdate, Decision, Suppression) into this shape so
// the notification package does not depend on internal/gateway.
type BuildInput struct {
	Repo        string // logical repo name (Policy.Repo)
	UpstreamURL string // Policy.UpstreamURL
	Observed    bool   // would-have-rejected under enforcement (observe mode)
	Resolved    bool   // clean push closed a prior fix-loop → "push.resolved"
	Refs        []BuildRef
	Findings    []BuildFinding
	Suppressed  []BuildSuppression
}

// BuildRef mirrors gateway.RefUpdate in the notification package's vocabulary.
type BuildRef struct {
	Name   string
	OldSHA string
	NewSHA string
}

// BuildFinding mirrors gateway.Finding plus the human-readable Messages line
// (when the gateway formatted one), letting Build parse file/line out of it.
type BuildFinding struct {
	FrameID  string
	Severity string
	Message  string
}

// BuildSuppression mirrors gateway.Suppression.
type BuildSuppression struct {
	FrameID string
	File    string
	Label   string
}

// Build constructs a Notification from gateway-side decision context. The
// caller has already converted its internal types into BuildInput so this
// package does not import internal/gateway (avoids the cycle).
//
// Event selection: accepted=false + observed=false → "push.rejected"; observed=true → "push.observed".
// (Build is only called when something was at least flagged; pure accepts
// don't fire notifications per spec.)
//
// EventID format: "evt_<UTC-RFC3339-with-dashes>_<8-hex-chars>".
//
// Finding.File / Finding.Line are derived best-effort by parsing the leading
// "file:line" token of the gateway's BuildFinding.Message (the canonical
// "file:line - label" shape produced by engine.Hit.Format and propagated
// through gateway.findingMessage). When the message doesn't carry that prefix,
// File stays empty + Line stays 0. Hint is left empty in v0.1 - frames don't
// yet carry structured hints.
//
// Fingerprint is computed via Fingerprint(frameID, file, line) - stable per
// (frame, file, line) triple so the loop state machine can detect "same
// finding twice" across pushes.
func Build(in BuildInput, gatewayVersion, instanceID string) Notification {
	event := "push.rejected"
	if in.Observed {
		event = "push.observed"
	}
	if in.Resolved {
		event = "push.resolved"
	}

	refs := make([]RefInfo, 0, len(in.Refs))
	for _, r := range in.Refs {
		refs = append(refs, RefInfo{
			Name:   r.Name,
			OldSHA: r.OldSHA,
			NewSHA: r.NewSHA,
			Type:   refUpdateType(r.OldSHA, r.NewSHA),
		})
	}

	findings := make([]Finding, 0, len(in.Findings))
	for _, f := range in.Findings {
		file, line := parseFileLine(f.Message)
		findings = append(findings, Finding{
			FrameID:     f.FrameID,
			Severity:    f.Severity,
			Message:     f.Message,
			File:        file,
			Line:        line,
			Fingerprint: Fingerprint(f.FrameID, file, line),
		})
	}

	suppressed := make([]SuppressedFinding, 0, len(in.Suppressed))
	for _, s := range in.Suppressed {
		suppressed = append(suppressed, SuppressedFinding{
			FrameID: s.FrameID,
			File:    s.File,
			Reason:  s.Label,
		})
	}

	now := time.Now().UTC()
	return Notification{
		SchemaVersion: SchemaVersion,
		Event:         event,
		EventID:       newEventID(now),
		Gateway: GatewayInfo{
			Name:       "nimblegate",
			Version:    gatewayVersion,
			InstanceID: instanceID,
		},
		Repo: RepoInfo{Name: in.Repo, UpstreamURL: in.UpstreamURL},
		Push: PushInfo{
			Timestamp: now,
			Refs:      refs,
		},
		Decision: DecisionInfo{
			Accepted:   in.Observed, // observed mode relays, so "accepted" wire-wise
			Observed:   in.Observed,
			Findings:   findings,
			Suppressed: suppressed,
		},
	}
}

const zeroRev = "0000000000000000000000000000000000000000"

func refUpdateType(old, new string) string {
	if old == zeroRev {
		return "create"
	}
	if new == zeroRev {
		return "delete"
	}
	return "update"
}

// fileLineRe matches the first "path:line " or "path:line -" token in a
// gateway-formatted finding message. Captures (file, line). Tolerates paths
// with slashes/dots. NOT anchored at start: several frames prefix the message
// with a summary ("pipe-to-shell patterns detected: deploy.sh:1 - …"), so the
// file:line sits mid-string - anchoring left the comment's Location cell empty.
var fileLineRe = regexp.MustCompile(`([^\s:]+):(\d+)(?:\s|$)`)

func parseFileLine(msg string) (string, int) {
	msg = strings.TrimSpace(msg)
	m := fileLineRe.FindStringSubmatch(msg)
	if m == nil {
		return "", 0
	}
	line, err := strconv.Atoi(m[2])
	if err != nil {
		return "", 0
	}
	return m[1], line
}

// newEventID returns "evt_<UTC-stamp>_<8-hex>" - the webhook idempotency key
// and the queue record ID. Stamp uses dashes instead of colons so the ID is
// filename-safe.
func newEventID(now time.Time) string {
	stamp := now.UTC().Format("2006-01-02T15-04-05Z")
	var b [4]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("evt_%s_%s", stamp, hex.EncodeToString(b[:]))
}
