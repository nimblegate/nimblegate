// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"nimblegate/internal/config"
)

// LinterPolicy is the [linters.*] portion of a repo's gateway-held appframes.toml.
type LinterPolicy struct {
	Linters map[string]config.LinterConfig
}

// LoadLinterPolicy reads the repo's appframes.toml linters via config.LoadProject.
// Missing file → empty policy, no error.
func LoadLinterPolicy(policyRoot, repo string) (LinterPolicy, error) {
	if !safeRepoName(repo) {
		return LinterPolicy{}, fmt.Errorf("gateway: invalid repo name %q", repo)
	}
	path := framePolicyPath(policyRoot, repo) // same appframes.toml
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return LinterPolicy{Linters: map[string]config.LinterConfig{}}, nil
		}
		return LinterPolicy{}, err
	}
	cfg, err := config.LoadProject(path)
	if err != nil {
		return LinterPolicy{}, fmt.Errorf("gateway: load linter policy for %q: %w", repo, err)
	}
	m := map[string]config.LinterConfig{}
	for k, v := range cfg.Linters {
		m[k] = v
	}
	return LinterPolicy{Linters: m}, nil
}

func (p LinterPolicy) clone() LinterPolicy {
	m := map[string]config.LinterConfig{}
	for k, v := range p.Linters {
		m[k] = v
	}
	return LinterPolicy{Linters: m}
}

func (p LinterPolicy) With(name string, cfg config.LinterConfig) LinterPolicy {
	out := p.clone()
	out.Linters[name] = cfg
	return out
}

func (p LinterPolicy) Delete(name string) LinterPolicy {
	out := p.clone()
	delete(out.Linters, name)
	return out
}

func (p LinterPolicy) SetSeverity(name, sev string) LinterPolicy {
	out := p.clone()
	if c, ok := out.Linters[name]; ok {
		c.Severity = sev
		out.Linters[name] = c
	}
	return out
}

func (p LinterPolicy) SetEnabled(name string, enabled bool) LinterPolicy {
	out := p.clone()
	if c, ok := out.Linters[name]; ok {
		c.Enabled = enabled
		out.Linters[name] = c
	}
	return out
}

// Save writes the repo's appframes.toml preserving the existing frames section
// and the [time-estimates] section.
func (p LinterPolicy) Save(policyRoot, repo string) error {
	if !safeRepoName(repo) {
		return fmt.Errorf("gateway: invalid repo name %q", repo)
	}
	fp, err := LoadFramePolicy(policyRoot, repo)
	if err != nil {
		return err
	}
	te, err := LoadTimeEstimates(policyRoot, repo)
	if err != nil {
		return err
	}
	return writePolicyTOML(policyRoot, repo, fp, p, te)
}

// linterTOML mirrors config.LinterConfig with omitempty for clean output;
// Enabled is always emitted (its false value is meaningful).
type linterTOML struct {
	Kind     string   `toml:"kind,omitempty"`
	Enabled  bool     `toml:"enabled"`
	Severity string   `toml:"severity,omitempty"`
	Dir      string   `toml:"dir,omitempty"`
	Disable  []string `toml:"disable,omitempty"`
	Command  string   `toml:"command,omitempty"`
	Args     []string `toml:"args,omitempty"`
	Patterns []string `toml:"patterns,omitempty"`
	Regex    string   `toml:"regex,omitempty"`
}

// writePolicyTOML atomically (temp+rename) writes appframes.toml holding the
// frames policy, the linters policy, and the per-tier time-estimates override.
// Any section may be empty.
func writePolicyTOML(policyRoot, repo string, fp FramePolicy, lp LinterPolicy, te config.TimeEstimates) error {
	if !safeRepoName(repo) {
		return fmt.Errorf("gateway: invalid repo name %q", repo)
	}
	dir := filepath.Join(policyRoot, repo)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("[frames]\nenabled = [")
	for i, e := range fp.Enabled {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%q", e)
	}
	b.WriteString("]\n")
	ids := make([]string, 0, len(fp.Severity))
	for id := range fp.Severity {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		fmt.Fprintf(&b, "\n[frames.%q]\nseverity = %q\n", id, fp.Severity[id])
	}
	if te.Tier1 != nil || te.Tier2 != nil || te.Tier3 != nil || te.Tier4 != nil || te.Tier5 != nil || te.Tier6 != nil {
		b.WriteString("\n[time-estimates]\n")
		for tier, ptr := range []*float64{te.Tier1, te.Tier2, te.Tier3, te.Tier4, te.Tier5, te.Tier6} {
			if ptr != nil {
				fmt.Fprintf(&b, "tier-%d = %g\n", tier+1, *ptr)
			}
		}
	}
	if len(lp.Linters) > 0 {
		conv := map[string]linterTOML{}
		for name, c := range lp.Linters {
			conv[name] = linterTOML{
				Kind: c.Kind, Enabled: c.Enabled, Severity: c.Severity, Dir: c.Dir,
				Disable: c.Disable, Command: c.Command, Args: c.Args, Patterns: c.Patterns, Regex: c.Regex,
			}
		}
		var buf bytes.Buffer
		if err := toml.NewEncoder(&buf).Encode(struct {
			Linters map[string]linterTOML `toml:"linters"`
		}{conv}); err != nil {
			return err
		}
		b.WriteString("\n")
		b.Write(buf.Bytes())
	}

	tmp, err := os.CreateTemp(dir, ".appframes.toml.*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.WriteString(b.String()); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, framePolicyPath(policyRoot, repo))
}
