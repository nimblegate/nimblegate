// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

// Package canonical loads TOML canonical tables shared across frames.
// A canonical table is a TOML file under .appframes/_canonical/ that frames
// reference via the `canonical-refs:` frontmatter field.
package canonical

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// Table wraps a parsed canonical TOML file as a tree of section -> key -> string-value.
// Each top-level [section] becomes a Section. Non-string scalar values are stringified.
type Table struct {
	sections map[string]map[string]string
}

// Section returns the key/value map for a top-level [section] of the table.
func (t Table) Section(name string) (map[string]string, bool) {
	s, ok := t.sections[name]
	return s, ok
}

// Load reads a canonical TOML table from path.
func Load(path string) (Table, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Table{}, fmt.Errorf("canonical: read %s: %w", path, err)
	}
	var raw map[string]any
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return Table{}, fmt.Errorf("canonical: parse %s: %w", path, err)
	}
	sections := map[string]map[string]string{}
	for k, v := range raw {
		section, ok := v.(map[string]any)
		if !ok {
			continue
		}
		out := map[string]string{}
		for sk, sv := range section {
			out[sk] = fmt.Sprintf("%v", sv)
		}
		sections[k] = out
	}
	return Table{sections: sections}, nil
}
