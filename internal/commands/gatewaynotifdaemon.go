// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"context"
	"fmt"
	"strings"
	"time"

	"nimblegate/internal/gateway"
	"nimblegate/internal/gateway/notification"
	"nimblegate/internal/gateway/notification/render"
	"nimblegate/internal/gateway/upstream"
	"nimblegate/internal/gateway/webhook"
)

// startNotificationDaemon launches the background drain loop that delivers
// queued PR-comment + webhook notifications. The pre-receive hook writes one
// queue record per rejected push (gateway.fireNotification); this loop polls
// those queues and delivers each through the orchestrator - posting/updating
// the sticky PR comment and firing the webhook.
//
// Wiring note: the Daemon + Orchestrator have existed since the rail was built
// but were never started from any running process, so the entire rail was inert
// regardless of per-repo config. This is the missing "wired alongside the
// dashboard server" step.
//
// The upstream registry is rebuilt every tick from the current set of repos +
// credentials, so newly-registered repos and rotated tokens are picked up
// without a dashboard restart.
func startNotificationDaemon(ctx context.Context, policyRoot string) {
	orch := &notification.Orchestrator{
		Webhook:    webhook.NewClient(),
		Render:     render.Comment,
		PolicyRoot: policyRoot,
	}
	// Start from the spec defaults (20 attempts; 1m/5m/30m/2h backoff) and only
	// override InlineRaceGap. Building the config from scratch is a trap:
	// resolveDefaults only fills defaults when PollInterval is zero, so a
	// hand-built config with a non-zero PollInterval leaves DeliveryMaxAttempts
	// at 0 - which makes drainQueue deadletter on the FIRST failure
	// (attempts >= 0 is always true) instead of retrying.
	cfg := notification.DefaultDaemonConfig()
	cfg.InlineRaceGap = 3 * time.Second // no inline delivery exists to race, so don't wait the full 30s
	d := &notification.Daemon{
		PolicyRoot:   policyRoot,
		Orchestrator: orch,
		Config:       cfg,
	}
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Refresh adapters each tick so new repos / rotated creds apply.
				orch.Upstreams = buildNotifRegistry(policyRoot)
				_ = d.PollOnce(ctx)
			}
		}
	}()
}

// buildNotifRegistry constructs one upstream adapter per registered repo that
// has notifications enabled, keyed by the repo's upstream URL and using that
// repo's stored credential. github.com hosts get the GitHub adapter; everything
// else uses the Gitea adapter (the two shipped adapters). The exact upstream URL
// is registered as an override prefix so DeliverOne's LookupByURL resolves the
// right per-repo adapter (and credential) for the queued record.
func buildNotifRegistry(policyRoot string) *upstream.Registry {
	reg := upstream.NewRegistry()
	for _, repo := range listGatewayRepos(policyRoot) {
		pol, err := (gateway.FilePolicyStore{Root: policyRoot}).Load(repo)
		if err != nil || pol.UpstreamURL == "" {
			continue
		}
		if pol.Notification == nil || !pol.Notification.Enabled {
			continue
		}
		cred, _ := (gateway.FileCredentialStore{Root: policyRoot}).Load(repo)
		name := "repo:" + repo
		var adapter upstream.Upstream
		if isGitHubURL(pol.UpstreamURL) {
			adapter = upstream.NewGitHubAdapter(pol.UpstreamURL, cred)
		} else {
			adapter = upstream.NewGiteaAdapter(pol.UpstreamURL, cred)
		}
		reg.Register(name, adapter)
		reg.RegisterOverride(pol.UpstreamURL, name)
	}
	return reg
}

// isGitHubURL picks the GitHub adapter for github.com upstreams; all other
// hosts (self-hosted Gitea, on-prem) use the Gitea adapter.
func isGitHubURL(u string) bool {
	return strings.Contains(u, "github.com")
}

// notifDaemonStartLine is the one-line startup banner, kept next to the daemon
// so the message and the behavior stay in sync.
func notifDaemonStartLine() string {
	return fmt.Sprintf("  notification rail: draining PR-comment + webhook queue every %s", 5*time.Second)
}
