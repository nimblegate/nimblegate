// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"nimblegate/internal/config"
)

// FramePolicy is the gateway-held frames policy for one repo - the part of the
// per-repo appframes.toml the tuning UI manages: the enabled frame/group list
// (preserved untouched) and per-frame severity overrides.
type FramePolicy struct {
	Enabled  []string
	Severity map[string]string // frame id -> "BLOCK"/"WARN"/"INFO"
}

func framePolicyPath(policyRoot, repo string) string {
	return filepath.Join(policyRoot, repo, "appframes.toml")
}

// LoadFramePolicy reads <policyRoot>/<repo>/appframes.toml via the same parser
// the engine uses. Missing file → empty policy, no error.
func LoadFramePolicy(policyRoot, repo string) (FramePolicy, error) {
	if !safeRepoName(repo) {
		return FramePolicy{}, fmt.Errorf("gateway: invalid repo name %q", repo)
	}
	path := framePolicyPath(policyRoot, repo)
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return FramePolicy{Severity: map[string]string{}}, nil
		}
		return FramePolicy{}, err
	}
	cfg, err := config.LoadProject(path)
	if err != nil {
		return FramePolicy{}, fmt.Errorf("gateway: load frame policy for %q: %w", repo, err)
	}
	sev := map[string]string{}
	for id, ov := range cfg.FrameOverrides {
		if ov.Severity != "" {
			sev[id] = ov.Severity
		}
	}
	return FramePolicy{Enabled: append([]string{}, cfg.Frames.Enabled...), Severity: sev}, nil
}

// LoadTimeEstimates reads <policyRoot>/<repo>/appframes.toml and returns its
// [time-estimates] section (per-hit hours overrides used to weight prevented
// findings). Missing file → zero value (all tier defaults), no error - matching
// LoadFramePolicy's missing-file behavior.
func LoadTimeEstimates(policyRoot, repo string) (config.TimeEstimates, error) {
	if !safeRepoName(repo) {
		return config.TimeEstimates{}, fmt.Errorf("gateway: invalid repo name %q", repo)
	}
	path := framePolicyPath(policyRoot, repo)
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return config.TimeEstimates{}, nil
		}
		return config.TimeEstimates{}, err
	}
	cfg, err := config.LoadProject(path)
	if err != nil {
		return config.TimeEstimates{}, fmt.Errorf("gateway: load time-estimates for %q: %w", repo, err)
	}
	return cfg.TimeEstimates, nil
}

// WithSeverity returns a copy with frame's severity set, preserving Enabled and
// all other overrides (does not mutate the receiver).
func (p FramePolicy) WithSeverity(frameID, severity string) FramePolicy {
	out := FramePolicy{Enabled: append([]string{}, p.Enabled...), Severity: map[string]string{}}
	for k, v := range p.Severity {
		out.Severity[k] = v
	}
	out.Severity[frameID] = severity
	return out
}

// Save writes the policy atomically (temp + rename), preserving any existing
// linters and [time-estimates] sections. Routes through writePolicyTOML so all
// three sections coexist.
func (p FramePolicy) Save(policyRoot, repo string) error {
	if !safeRepoName(repo) {
		return fmt.Errorf("gateway: invalid repo name %q", repo)
	}
	lp, err := LoadLinterPolicy(policyRoot, repo)
	if err != nil {
		return err
	}
	te, err := LoadTimeEstimates(policyRoot, repo)
	if err != nil {
		return err
	}
	return writePolicyTOML(policyRoot, repo, p, lp, te)
}

// SaveTimeEstimates writes new per-tier hours overrides while preserving the
// existing [frames] and [linters] sections. te is the full replacement -
// any tier whose pointer is nil reverts to the built-in default.
func SaveTimeEstimates(policyRoot, repo string, te config.TimeEstimates) error {
	if !safeRepoName(repo) {
		return fmt.Errorf("gateway: invalid repo name %q", repo)
	}
	fp, err := LoadFramePolicy(policyRoot, repo)
	if err != nil {
		return err
	}
	lp, err := LoadLinterPolicy(policyRoot, repo)
	if err != nil {
		return err
	}
	return writePolicyTOML(policyRoot, repo, fp, lp, te)
}
