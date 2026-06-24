// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package v2

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Kit is one entry in the v2 stdlib catalog OR an operator-defined custom
// kit. Kits hold AXIS SELECTIONS that the operator's `appframes.toml` will
// receive when the kit is applied (set-unioned per spec §7.3).
type Kit struct {
	KitID       string     `toml:"kit_id"`
	Display     string     `toml:"display"`
	Semver      string     `toml:"semver"`
	Description string     `toml:"description"`
	Selections  Selections `toml:"selections"`
}

// Selections is the axis-selection bundle a kit applies. Mirrors the same
// shape as v2.Config's axis sections.
type Selections struct {
	Framework       string              `toml:"framework"`
	Platform        string              `toml:"platform"`
	PlatformExclude map[string][]string `toml:"platform_exclude"`
	Domains         []string            `toml:"domains"`
}

// kitFile is the wrapper around the TOML's [[kit]] array; the file's top
// level is a single key kit = [{...}, ...].
type kitFile struct {
	Kits []Kit `toml:"kit"`
}

// Set is the loaded registry of v2 kits, keyed by kit_id for O(1) lookup.
type Set struct {
	defs map[string]Kit
}

// LoadStdlib parses the embedded v2 stdlib.toml. Enforces unique kit_id
// (decision #16: flat IDs with duplicate-check at creation time).
func LoadStdlib() (*Set, error) {
	data, err := stdlibFS.ReadFile("stdlib.toml")
	if err != nil {
		return nil, fmt.Errorf("kits v2: read stdlib.toml: %w", err)
	}
	var raw kitFile
	if err := toml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("kits v2: parse stdlib.toml: %w", err)
	}
	defs := make(map[string]Kit, len(raw.Kits))
	for _, k := range raw.Kits {
		if k.KitID == "" {
			return nil, fmt.Errorf("kits v2: kit with empty kit_id (display=%q)", k.Display)
		}
		if k.Display == "" {
			return nil, fmt.Errorf("kits v2: kit %q missing display", k.KitID)
		}
		if k.Semver == "" {
			return nil, fmt.Errorf("kits v2: kit %q missing semver", k.KitID)
		}
		if _, dup := defs[k.KitID]; dup {
			return nil, fmt.Errorf("kits v2: duplicate kit_id %q", k.KitID)
		}
		defs[k.KitID] = k
	}
	return &Set{defs: defs}, nil
}

// Get returns a kit by kit_id. The bool reports whether it was found.
func (s *Set) Get(kitID string) (Kit, bool) {
	k, ok := s.defs[kitID]
	return k, ok
}

// All returns every kit in the registry, sorted by kit_id for stable output.
func (s *Set) All() []Kit {
	out := make([]Kit, 0, len(s.defs))
	for _, k := range s.defs {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].KitID < out[j].KitID })
	return out
}

// IDs returns every kit_id in the registry, sorted.
func (s *Set) IDs() []string {
	ids := make([]string, 0, len(s.defs))
	for id := range s.defs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Hash returns the SHA256 fingerprint of the kit's effective content
// (kit_id + semver + selections in deterministic order). Used for the
// hybrid semver+hash versioning per spec decision #18 - notifications
// fire on semver delta; hash detects cosmetic changes.
func (k Kit) Hash() string {
	var b strings.Builder
	b.WriteString("id:")
	b.WriteString(k.KitID)
	b.WriteString("\nsemver:")
	b.WriteString(k.Semver)
	b.WriteString("\nframework:")
	b.WriteString(k.Selections.Framework)
	b.WriteString("\nplatform:")
	b.WriteString(k.Selections.Platform)
	// platform_exclude - sort keys + values for deterministic serialization
	if len(k.Selections.PlatformExclude) > 0 {
		vendors := make([]string, 0, len(k.Selections.PlatformExclude))
		for v := range k.Selections.PlatformExclude {
			vendors = append(vendors, v)
		}
		sort.Strings(vendors)
		for _, v := range vendors {
			excludes := append([]string{}, k.Selections.PlatformExclude[v]...)
			sort.Strings(excludes)
			b.WriteString("\nplatform_exclude.")
			b.WriteString(v)
			b.WriteString(":")
			b.WriteString(strings.Join(excludes, ","))
		}
	}
	// domains
	domains := append([]string{}, k.Selections.Domains...)
	sort.Strings(domains)
	b.WriteString("\ndomains:")
	b.WriteString(strings.Join(domains, ","))

	sum := sha256.Sum256([]byte(b.String()))
	return "sha256:" + hex.EncodeToString(sum[:])
}
