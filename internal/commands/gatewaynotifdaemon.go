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
// The upstream registry and webhook targets are rebuilt every tick from the
// current set of repos, credentials and gateway.toml files, so newly-registered
// repos, rotated tokens and a corrected webhook URL or secret are picked up
// without a dashboard restart - including for records already queued.
func startNotificationDaemon(ctx context.Context, policyRoot string) {
	orch := newNotifOrchestrator(policyRoot)
	// Start from the spec defaults (30s race gap behind the hook's inline
	// attempt; 20 attempts; 1m/5m/30m/2h backoff). Building the config from
	// scratch is a trap: resolveDefaults only fills defaults when PollInterval
	// is zero, so a hand-built config with a non-zero PollInterval leaves
	// DeliveryMaxAttempts at 0 - which makes drainQueue deadletter on the FIRST
	// failure (attempts >= 0 is always true) instead of retrying.
	d := &notification.Daemon{
		PolicyRoot:   policyRoot,
		Orchestrator: orch,
		Config:       notification.DefaultDaemonConfig(),
	}
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				orch.Upstreams, orch.Webhooks = buildNotifConfig(policyRoot)
				_ = d.PollOnce(ctx)
			}
		}
	}()
}

func newNotifOrchestrator(policyRoot string) *notification.Orchestrator {
	return &notification.Orchestrator{
		Webhook:    webhook.NewClient(),
		Render:     render.Comment,
		PolicyRoot: policyRoot,
	}
}

// buildNotifConfig constructs one upstream adapter per registered repo that
// has notifications enabled, using that repo's stored credential, plus each
// such repo's current webhook target.
func buildNotifConfig(policyRoot string) (*upstream.Registry, map[string]notification.WebhookTarget) {
	reg := upstream.NewRegistry()
	hooks := map[string]notification.WebhookTarget{}
	for _, repo := range listGatewayRepos(policyRoot) {
		pol, err := (gateway.FilePolicyStore{Root: policyRoot}).Load(repo)
		if err != nil || pol.Notification == nil || !pol.Notification.Enabled {
			continue
		}
		hooks[repo] = webhookTarget(pol.Notification)
		if pol.UpstreamURL == "" {
			continue
		}
		cred, _ := (gateway.FileCredentialStore{Root: policyRoot}).Load(repo)
		registerNotifAdapter(reg, repo, pol.UpstreamURL, cred)
	}
	return reg, hooks
}

// inlineNotifOrchestrator builds the orchestrator the pre-receive hook uses
// for its inline delivery attempt, covering just this repo. Nil (skip inline,
// the daemon delivers) when the rail is off or the hook user cannot read the
// upstream credential, as with a privilege-separated relay.
func inlineNotifOrchestrator(policyRoot, repo string, pol gateway.Policy) *notification.Orchestrator {
	if pol.Notification == nil || !pol.Notification.Enabled {
		return nil
	}
	reg := upstream.NewRegistry()
	if pol.UpstreamURL != "" {
		cred, err := (gateway.FileCredentialStore{Root: policyRoot}).Load(repo)
		if err != nil {
			return nil
		}
		registerNotifAdapter(reg, repo, pol.UpstreamURL, cred)
	}
	o := newNotifOrchestrator(policyRoot)
	o.Upstreams = reg
	o.Webhooks = map[string]notification.WebhookTarget{repo: webhookTarget(pol.Notification)}
	return o
}

func webhookTarget(nc *gateway.NotificationConfig) notification.WebhookTarget {
	return notification.WebhookTarget{URL: nc.WebhookURL, Auth: nc.WebhookAuth}
}

// registerNotifAdapter registers repo's adapter: github.com hosts get the
// GitHub adapter; everything else uses the Gitea adapter (the two shipped
// adapters). The exact upstream URL is registered as an override prefix so
// DeliverOne's LookupByURL resolves the right per-repo adapter (and
// credential) for the queued record.
func registerNotifAdapter(reg *upstream.Registry, repo, upstreamURL, cred string) {
	name := "repo:" + repo
	var adapter upstream.Upstream
	if isGitHubURL(upstreamURL) {
		adapter = upstream.NewGitHubAdapter(upstreamURL, cred)
	} else {
		adapter = upstream.NewGiteaAdapter(upstreamURL, cred)
	}
	reg.Register(name, adapter)
	reg.RegisterOverride(upstreamURL, name)
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
