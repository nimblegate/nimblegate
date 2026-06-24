// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// IssueSeverity classifies a skeleton-verify finding by how broken the repo is
// in its current on-disk state.
type IssueSeverity string

const (
	// IssueBlocking - the repo cannot function in its current state. Pushes or
	// relays will fail outright. Operator action required.
	IssueBlocking IssueSeverity = "blocking"

	// IssueDegraded - pushes still work, but a dashboard feature will misbehave
	// or appear empty. Auto-repair available for most.
	IssueDegraded IssueSeverity = "degraded"
)

// SkeletonIssue is one missing or malformed file the gateway expected to find
// for the given repo. Repair, when non-empty, names a canonical operation
// that fixes it; the dashboard renders a one-click Repair button per issue.
type SkeletonIssue struct {
	Repo     string        `json:"repo"`
	File     string        `json:"file"`     // human-readable file identifier ("appframes.toml")
	Severity IssueSeverity `json:"severity"` // blocking | degraded
	What     string        `json:"what"`     // what's wrong, one sentence
	Why      string        `json:"why"`      // why it matters, one sentence
	Repair   string        `json:"repair,omitempty"`
}

// Skeleton declares which files a fully-wired repo registration owns and how
// to seed defaults / verify they exist. Generate is called from AddRepo;
// Verify drives the dashboard "what's missing" banner; Repair runs the
// per-issue regen when the operator clicks the button.
type Skeleton struct {
	PolicyRoot string
	ReposRoot  string
}

// defaultAppframesTOML is the seed content for a freshly-registered repo's
// appframes.toml. Empty [frames] section - every subsequent write through
// writePolicyTOML preserves this shape, so the dashboard's kit/frame
// handlers find a parseable file instead of a missing-file silent no-op.
func defaultAppframesTOML() []byte {
	return []byte("[frames]\nenabled = []\n")
}

// Generate seeds default files for a freshly-registered repo. Called from
// AddRepo AFTER writeGatewayTOML - so gateway.toml is assumed present.
// Idempotent: each step no-ops if its file already exists, so calling
// Generate twice on the same repo is safe.
func (s Skeleton) Generate(repo string) error {
	path := framePolicyPath(s.PolicyRoot, repo)
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("gateway: stat appframes.toml for %q: %w", repo, err)
	}
	if err := os.WriteFile(path, defaultAppframesTOML(), 0o644); err != nil {
		return fmt.Errorf("gateway: seed appframes.toml for %q: %w", repo, err)
	}
	return nil
}

// Verify returns issues with the on-disk state for the named repo. Empty list
// = fully-wired. Caller decides how to surface (dashboard banner, API
// response, CLI output). Read-only; never mutates anything.
//
// Order is roughly most-blocking-first: missing bare or gateway.toml means
// nothing else works, so subsequent checks short-circuit.
func (s Skeleton) Verify(repo string) ([]SkeletonIssue, error) {
	var issues []SkeletonIssue

	bare := filepath.Join(s.ReposRoot, repo+".git")
	if _, err := os.Stat(bare); errors.Is(err, fs.ErrNotExist) {
		issues = append(issues, SkeletonIssue{
			Repo: repo, File: repo + ".git", Severity: IssueBlocking,
			What: "Bare repo missing from repos-root",
			Why:  "Pushes will be rejected immediately. Restore from _repos/ or re-register the repo via the dashboard.",
		})
	} else if err != nil {
		return nil, err
	}

	store := FilePolicyStore{Root: s.PolicyRoot}
	gwPath := store.file(repo)
	if _, err := os.Stat(gwPath); errors.Is(err, fs.ErrNotExist) {
		issues = append(issues, SkeletonIssue{
			Repo: repo, File: "gateway.toml", Severity: IssueBlocking,
			What: "Per-repo gateway.toml missing",
			Why:  "Gateway can't determine upstream URL or protected refs - every push will fail to relay.",
		})
		return issues, nil
	} else if err != nil {
		return nil, err
	}

	pol, err := store.Load(repo)
	if err != nil {
		return nil, fmt.Errorf("gateway: load policy for %q: %w", repo, err)
	}

	fpPath := framePolicyPath(s.PolicyRoot, repo)
	if _, err := os.Stat(fpPath); errors.Is(err, fs.ErrNotExist) {
		issues = append(issues, SkeletonIssue{
			Repo: repo, File: "appframes.toml", Severity: IssueDegraded,
			What:   "Per-repo appframes.toml missing",
			Why:    "Dashboard kit / frame controls read this file; missing means the policy view starts empty and clicks may appear to no-op until the first save creates it.",
			Repair: "regen-nimblegate-toml",
		})
	} else if err != nil {
		return nil, err
	}

	credPath := filepath.Join(s.PolicyRoot, repo, "credential")
	credExists := skeletonFileExists(credPath)
	if !IsSSHUpstream(pol.UpstreamURL) && !credExists {
		issues = append(issues, SkeletonIssue{
			Repo: repo, File: "credential", Severity: IssueBlocking,
			What: "HTTP upstream URL without credential file",
			Why:  "Relays will fail with 401 / Permission denied. Install a PAT via the Rotate upstream credential form.",
		})
	}

	// Seed-pending marker: dropped by the add flow when the registration-time
	// mirror of the upstream didn't complete (upstream unreachable, or an http
	// upstream whose credential wasn't set yet). Surfaces as a one-click "Sync
	// from upstream" repair so the operator never has to SSH in and run a fetch
	// by hand. Marker-gated rather than "0 branches" so a genuinely-new repo
	// with an empty upstream doesn't false-positive.
	if pol.UpstreamURL != "" && skeletonFileExists(filepath.Join(s.PolicyRoot, repo, seedPendingMarker)) {
		issues = append(issues, SkeletonIssue{
			Repo: repo, File: "upstream history", Severity: IssueDegraded,
			What:   "Gateway not yet seeded from the upstream's existing history",
			Why:    "The upstream has commits the gateway hasn't mirrored, so cloning from the gateway returns an empty repo. For an HTTP upstream, set the credential first, then Sync to pull the history.",
			Repair: "sync-from-upstream",
		})
	}

	return issues, nil
}

// Repair runs the targeted regen for one issue. Returns an error if the
// operation name is unknown or the regen fails. Called from the dashboard
// when an operator clicks the per-finding Repair button.
//
// Operations without auto-repair (missing bare repo, missing gateway.toml,
// missing credential) return an explicit error rather than silently
// succeeding - the operator needs to know the dashboard can't fix it.
func (s Skeleton) Repair(repo, operation string) error {
	switch operation {
	case "regen-nimblegate-toml":
		path := framePolicyPath(s.PolicyRoot, repo)
		if _, err := os.Stat(path); err == nil {
			return nil
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return os.WriteFile(path, defaultAppframesTOML(), 0o644)
	case "sync-from-upstream":
		pol, err := FilePolicyStore{Root: s.PolicyRoot}.Load(repo)
		if err != nil {
			return err
		}
		cred, _ := FileCredentialStore{Root: s.PolicyRoot}.Load(repo)
		bare := filepath.Join(s.ReposRoot, repo+".git")
		if _, err := SeedFromUpstream(bare, pol.UpstreamURL, cred); err != nil {
			return err
		}
		if err := os.Remove(filepath.Join(s.PolicyRoot, repo, seedPendingMarker)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	case "":
		return fmt.Errorf("issue has no auto-repair - operator action required")
	default:
		return fmt.Errorf("unknown repair operation: %q", operation)
	}
}

func skeletonFileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
