// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	v2 "nimblegate/internal/config/v2"
	v2kits "nimblegate/internal/kits/v2"
	"nimblegate/internal/migration/v2apply"
	"nimblegate/internal/paths"
)

// kitsV2List prints the v2 kit catalog. Falls through from `kits list` when
// the operator's config is schema v2.
func kitsV2List(args []string) int {
	fs := flag.NewFlagSet("kits list (v2)", flag.ContinueOnError)
	_ = fs.Parse(args)

	set, err := v2kits.LoadStdlib()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate kits list: %v\n", err)
		return 2
	}

	// Mark which kits are recorded as applied in the operator's config.
	applied := make(map[string]v2.AppliedKit)
	if cwd, err := os.Getwd(); err == nil {
		if root, err := paths.FindProjectRoot(cwd); err == nil {
			cfgPath := filepath.Join(root, "appframes.toml")
			if cfg, err := v2.Load(cfgPath); err == nil {
				for _, ak := range cfg.Meta.AppliedKits {
					applied[ak.ID] = ak
				}
			}
		}
	}

	fmt.Println("v2 stdlib kits:")
	for _, k := range set.All() {
		marker := " "
		if ak, ok := applied[k.KitID]; ok {
			if ak.Semver != k.Semver {
				marker = "↑" // update available
			} else {
				marker = "✓"
			}
		}
		fmt.Printf("  %s %s (%s): v%s\n", marker, k.KitID, k.Display, k.Semver)
		if k.Description != "" {
			fmt.Printf("       %s\n", k.Description)
		}
	}
	fmt.Println()
	fmt.Println("legend: ✓ applied at current version  ↑ applied but stdlib has newer version")
	return 0
}

// kitsV2Apply applies a v2 stdlib kit to the operator's v2 appframes.toml.
// Falls through from `kits apply` when the operator's config is schema v2.
func kitsV2Apply(args []string) int {
	fs := flag.NewFlagSet("kits apply (v2)", flag.ContinueOnError)
	overwrite := fs.Bool("overwrite", false, "replace conflicting single-select axes (framework, platform) with the kit's values; default merge mode errors on conflict")
	dryRun := fs.Bool("dry-run", false, "show the resulting config without writing it")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: nimblegate kits apply <kit_id> [--overwrite] [--dry-run]")
		return 2
	}
	kitID := fs.Arg(0)

	set, err := v2kits.LoadStdlib()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate kits apply: %v\n", err)
		return 2
	}
	kit, ok := set.Get(kitID)
	if !ok {
		fmt.Fprintf(os.Stderr, "nimblegate kits apply: unknown kit %q. Run `nimblegate kits list` to see available.\n", kitID)
		return 2
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate kits apply: getwd: %v\n", err)
		return 2
	}
	root, err := paths.FindProjectRoot(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate kits apply: %v\n", err)
		return 2
	}
	cfgPath := filepath.Join(root, "appframes.toml")
	cfg, err := v2.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate kits apply: load v2 config: %v\n", err)
		return 2
	}

	mode := v2apply.ModeMerge
	if *overwrite {
		mode = v2apply.ModeOverwrite
	}

	res, err := v2apply.Apply(cfg, kit, mode, time.Now())
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate kits apply: %v\n", err)
		return 1
	}
	for _, w := range res.Warnings {
		fmt.Printf("  ⚠ %s\n", w)
	}

	if *dryRun {
		rendered, rerr := renderKitsV2TOML(cfg)
		if rerr != nil {
			fmt.Fprintf(os.Stderr, "render: %v\n", rerr)
			return 2
		}
		fmt.Println()
		fmt.Println("--- dry-run output (NOT WRITTEN) ---")
		fmt.Println(rendered)
		return 0
	}

	rendered, err := renderKitsV2TOML(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate kits apply: render: %v\n", err)
		return 2
	}
	if err := os.WriteFile(cfgPath, []byte(rendered), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate kits apply: write: %v\n", err)
		return 2
	}
	fmt.Printf("✓ Applied kit %q (v%s) to %s\n", kit.KitID, kit.Semver, cfgPath)
	return 0
}

// kitsV2Status is implemented in kitsv2_status.go (Phase D Task D3).
// Stub declared here so kits.go's dispatch can reference it.
var kitsV2Status = kitsV2StatusImpl

// renderKitsV2TOML serializes a v2.Config to its TOML representation,
// including the [[meta.applied_kit]] array. Reused from migrate-config's
// renderer pattern but with applied-kit array support.
func renderKitsV2TOML(cfg *v2.Config) (string, error) {
	var b bytes.Buffer

	b.WriteString("[appframes.schema]\n")
	fmt.Fprintf(&b, "version = %d\n\n", cfg.Appframes.Schema.Version)

	if cfg.Framework.Selected != "" {
		b.WriteString("[framework]\n")
		fmt.Fprintf(&b, "selected = %q\n\n", cfg.Framework.Selected)
	}

	if cfg.Platform.Selected != "" {
		b.WriteString("[platform]\n")
		fmt.Fprintf(&b, "selected = %q\n\n", cfg.Platform.Selected)
		for vendor, vo := range cfg.PlatformOverrides {
			if len(vo.Exclude) == 0 {
				continue
			}
			fmt.Fprintf(&b, "[platform.%s]\n", vendor)
			b.WriteString("exclude = [")
			parts := make([]string, len(vo.Exclude))
			for i, e := range vo.Exclude {
				parts[i] = fmt.Sprintf("%q", e)
			}
			b.WriteString(strings.Join(parts, ", "))
			b.WriteString("]\n\n")
		}
	}

	if len(cfg.Domains.Selected) > 0 {
		b.WriteString("[domains]\n")
		b.WriteString("selected = [")
		parts := make([]string, len(cfg.Domains.Selected))
		for i, d := range cfg.Domains.Selected {
			parts[i] = fmt.Sprintf("%q", d)
		}
		b.WriteString(strings.Join(parts, ", "))
		b.WriteString("]\n\n")
	}

	b.WriteString("[core]\n")
	fmt.Fprintf(&b, "enabled = %v\n\n", cfg.Core.Enabled)

	// [frames.overrides] - preserve any operator-set per-frame overrides.
	if len(cfg.Frames.Overrides) > 0 {
		b.WriteString("[frames.overrides]\n")
		// Stable key order for diff-friendliness.
		keys := make([]string, 0, len(cfg.Frames.Overrides))
		for k := range cfg.Frames.Overrides {
			keys = append(keys, k)
		}
		// Sort imported lazily here; deterministic across runs.
		for i := 0; i < len(keys); i++ {
			for j := i + 1; j < len(keys); j++ {
				if keys[i] > keys[j] {
					keys[i], keys[j] = keys[j], keys[i]
				}
			}
		}
		for _, k := range keys {
			ov := cfg.Frames.Overrides[k]
			var fields []string
			if ov.Severity != nil {
				fields = append(fields, fmt.Sprintf("severity = %q", *ov.Severity))
			}
			if ov.Enabled != nil {
				fields = append(fields, fmt.Sprintf("enabled = %v", *ov.Enabled))
			}
			if len(fields) > 0 {
				fmt.Fprintf(&b, "%q = { %s }\n", k, strings.Join(fields, ", "))
			}
		}
		b.WriteString("\n")
	}

	// [[meta.applied_kit]] - array of recorded applications.
	for _, ak := range cfg.Meta.AppliedKits {
		b.WriteString("[[meta.applied_kit]]\n")
		fmt.Fprintf(&b, "id = %q\n", ak.ID)
		fmt.Fprintf(&b, "semver = %q\n", ak.Semver)
		fmt.Fprintf(&b, "hash = %q\n", ak.Hash)
		fmt.Fprintf(&b, "applied_at = %q\n\n", ak.AppliedAt)
	}

	// Suppress unused-import warning for toml - kept in case future
	// callers want to use Marshal helpers.
	_ = toml.Marshal

	return b.String(), nil
}
