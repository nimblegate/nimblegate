// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

// Package kits is the stdlib starter-kit registry. A kit is a
// curated, fully-enumerated list of frame IDs that operators can
// apply (tick all) or clear (untick all) as a unit. Unlike the
// retired `groups` package, kits do NOT support recursive references,
// wildcards, or composition - each kit lists its frames explicitly,
// and resolution at runtime is a flat lookup.
package kits

import (
	"embed"
	"fmt"

	"github.com/BurntSushi/toml"
)

//go:embed stdlib.toml
var stdlibFS embed.FS

// Kit is one entry in stdlib.toml (or a user-defined custom kit).
type Kit struct {
	Name        string   `toml:"-"`
	Display     string   `toml:"display"`
	Description string   `toml:"description"`
	Frames      []string `toml:"frames"`
}

// Set is the loaded registry of kits, keyed by name.
type Set struct {
	defs map[string]Kit
}

// LoadStdlib parses the embedded stdlib.toml.
func LoadStdlib() (*Set, error) {
	data, err := stdlibFS.ReadFile("stdlib.toml")
	if err != nil {
		return nil, fmt.Errorf("kits: read stdlib.toml: %w", err)
	}
	raw := map[string]Kit{}
	if err := toml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("kits: parse stdlib.toml: %w", err)
	}
	defs := map[string]Kit{}
	for name, k := range raw {
		if k.Display == "" {
			return nil, fmt.Errorf("kits: %q is missing display", name)
		}
		if len(k.Frames) == 0 {
			return nil, fmt.Errorf("kits: %q has empty frames list", name)
		}
		k.Name = name
		defs[name] = k
	}
	return &Set{defs: defs}, nil
}

// Get returns a kit by name. The bool reports whether it was found.
func (s *Set) Get(name string) (Kit, bool) {
	k, ok := s.defs[name]
	return k, ok
}

// All returns every kit in the registry. Iteration order is not
// guaranteed; callers that need a stable order must sort by Name.
func (s *Set) All() []Kit {
	out := make([]Kit, 0, len(s.defs))
	for _, k := range s.defs {
		out = append(out, k)
	}
	return out
}

// Names returns the names of every kit, useful for CLI/dashboard listings.
func (s *Set) Names() []string {
	out := make([]string, 0, len(s.defs))
	for name := range s.defs {
		out = append(out, name)
	}
	return out
}
