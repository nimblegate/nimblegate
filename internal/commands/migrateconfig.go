// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	v2 "nimblegate/internal/config/v2"
	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
	"nimblegate/internal/kits"
	"nimblegate/internal/migration/v1tov2"
	"nimblegate/internal/paths"
	"nimblegate/internal/stdlib"
)

// MigrateConfig implements `nimblegate migrate-config` - reads the project's
// existing v1 appframes.toml and writes a v2 equivalent. See spec §9.2 for
// the user-facing output format.
//
// The command is intentionally conservative: it requires explicit operator
// invocation (no auto-migration on first run), creates a backup before
// writing, and refuses to overwrite an existing v2 config without --force.
//
// Zero-delta verification is INVOKED but currently returns a "not yet wired
// up" error indicating Phase F engine integration is the unblock. The
// migrate command surfaces this clearly - operator sees the translation
// happened but the runtime equivalence check is gated on later work.
func MigrateConfig(args []string) int {
	fs := flag.NewFlagSet("migrate-config", flag.ExitOnError)
	dryRun := fs.Bool("dry-run", false, "show the translated config without writing it")
	force := fs.Bool("force", false, "overwrite an existing v2 appframes.toml without prompting")
	skipBackup := fs.Bool("no-backup", false, "skip writing the .v1-backup file (default: write backup)")
	strict := fs.Bool("strict", false, "treat coverage loss as a hard error (default: warn and proceed)")
	_ = fs.Parse(args)

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate migrate-config: getwd: %v\n", err)
		return 2
	}
	root, err := paths.FindProjectRoot(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate migrate-config: %v\nHint: run from within a nimblegate project directory.\n", err)
		return 2
	}
	configPath := filepath.Join(root, "appframes.toml")

	// Probe schema version - if already v2 and not --force, refuse.
	if version, err := probeSchemaVersion(configPath); err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate migrate-config: probe %s: %v\n", configPath, err)
		return 2
	} else if version == 2 && !*force {
		fmt.Fprintf(os.Stderr, "nimblegate migrate-config: %s is already schema v2 (use --force to re-translate)\n", configPath)
		return 1
	}

	// Read the v1 [ui] applied_kits list.
	appliedKits, err := readV1AppliedKitsList(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate migrate-config: read applied_kits in %s: %v\n", configPath, err)
		return 2
	}

	fmt.Printf("nimblegate migrate-config: reading %s (schema v1)\n", configPath)
	fmt.Printf("  detected: applied_kits = %v\n", appliedKits)

	// Translate.
	result := v1tov2.TranslateWithErrors(v1tov2.Input{AppliedKits: appliedKits})
	for _, w := range result.Warnings {
		fmt.Printf("  ⚠ %s\n", w)
	}

	// Internal consistency check.
	if err := v1tov2.ValidateInternalConsistency(result.Config); err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate migrate-config: translated config failed internal consistency: %v\n", err)
		return 2
	}

	// Show the translation outcome.
	fmt.Println("  translating to v2:")
	fmt.Printf("    framework = %q\n", result.Config.Framework.Selected)
	fmt.Printf("    platform = %q\n", result.Config.Platform.Selected)
	for vendor, vo := range result.Config.PlatformOverrides {
		fmt.Printf("    platform.%s.exclude = %v\n", vendor, vo.Exclude)
	}
	fmt.Printf("    domains = %v\n", result.Config.Domains.Selected)

	// Zero-loss gate - Phase F integration uses engine.BuildV2FrameMap +
	// kits.LoadStdlib to do frame-set-level comparison.
	kitSet, kerr := kits.LoadStdlib()
	stdlibFrames, _ := stdlib.Load()
	v2Map, merr := engine.BuildV2FrameMap()
	if kerr != nil || merr != nil {
		fmt.Printf("  ⚠ zero-loss validation skipped (kit/frame-map load failed): kit=%v map=%v\n", kerr, merr)
	} else {
		resolver := func(cfg *v2.Config, fs []frames.Frame) []string {
			return v2Map.EnabledFrameIDs(cfg, fs)
		}
		zlRes, zerr := v1tov2.ValidateZeroLoss(appliedKits, kitSet, stdlibFrames, result.Config, resolver)
		if zerr != nil {
			fmt.Printf("  ⚠ zero-loss validation error: %v\n", zerr)
		} else {
			switch {
			case zlRes.Identical:
				fmt.Printf("  ✓ zero-loss confirmed (identical frame set): %s\n", zlRes.Explanation)
			case len(zlRes.OnlyInV1) == 0:
				fmt.Printf("  ✓ zero-loss confirmed: %s\n", zlRes.Explanation)
				if len(zlRes.OnlyInV2) > 0 {
					fmt.Printf("    v2 adds %d frames (intentional 'safer by default'): %v\n", len(zlRes.OnlyInV2), zlRes.OnlyInV2)
				}
			default:
				fmt.Printf("  ⚠ COVERAGE LOSS detected: these v1 frames are NOT in v2 under this config:\n")
				for _, id := range zlRes.OnlyInV1 {
					fmt.Printf("      - %s\n", id)
				}
				fmt.Println("    These frames are typically platform-coupled in v2 (e.g., cf-pages-specific); ")
				fmt.Println("    if your project doesn't use the missing platform, the frames wouldn't have")
				fmt.Println("    fired anyway. Add platform=<vendor> selection to recover them.")
				if *strict {
					fmt.Println("    --strict mode: refusing to write. Add the platform selection and re-run.")
					return 2
				}
				fmt.Println("    Proceeding with migration; use --strict to refuse on any loss.")
			}
		}
	}

	if *dryRun {
		// Render the v2 TOML to stdout instead of writing.
		fmt.Println()
		fmt.Println("--- dry-run output (NOT WRITTEN) ---")
		rendered, rerr := renderV2TOML(result.Config)
		if rerr != nil {
			fmt.Fprintf(os.Stderr, "nimblegate migrate-config: render: %v\n", rerr)
			return 2
		}
		fmt.Println(rendered)
		return 0
	}

	// Write backup before clobbering.
	if !*skipBackup {
		backupPath := configPath + ".v1-backup"
		if err := copyFile(configPath, backupPath); err != nil {
			fmt.Fprintf(os.Stderr, "nimblegate migrate-config: backup write %s: %v\n", backupPath, err)
			return 2
		}
		fmt.Printf("  backed up v1 config to %s\n", backupPath)
	}

	rendered, err := renderV2TOML(result.Config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate migrate-config: render v2: %v\n", err)
		return 2
	}
	if err := os.WriteFile(configPath, []byte(rendered), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate migrate-config: write %s: %v\n", configPath, err)
		return 2
	}
	fmt.Printf("  wrote %s (schema v2)\n", configPath)
	return 0
}

// probeSchemaVersion reads just the [appframes.schema].version field from the
// config. Returns 0 if the field is absent (implicit v1).
func probeSchemaVersion(path string) (int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var probe struct {
		Appframes struct {
			Schema struct {
				Version int `toml:"version"`
			} `toml:"schema"`
		} `toml:"appframes"`
	}
	if err := toml.Unmarshal(raw, &probe); err != nil {
		return 0, err
	}
	return probe.Appframes.Schema.Version, nil
}

// readV1AppliedKitsList pulls the [ui] applied_kits list from a v1 config. Returns
// an empty list if the section/field is absent (operator hasn't applied any kit
// explicitly - they get just the implicit core).
func readV1AppliedKitsList(path string) ([]string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var probe struct {
		UI struct {
			AppliedKits []string `toml:"applied_kits"`
		} `toml:"ui"`
	}
	if err := toml.Unmarshal(raw, &probe); err != nil {
		return nil, err
	}
	if len(probe.UI.AppliedKits) == 0 {
		return []string{"core"}, nil // implicit core when nothing applied
	}
	return probe.UI.AppliedKits, nil
}

// copyFile reads src and writes its contents to dst with mode 0644.
func copyFile(src, dst string) error {
	raw, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, raw, 0o644)
}

// renderV2TOML produces a v2.Config as a TOML document. Doesn't use
// burntsushi/toml's encoder because it can't represent map[string]VendorOverride
// as named subtables; hand-rolled to control the structure.
func renderV2TOML(cfg *v2.Config) (string, error) {
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
	fmt.Fprintf(&b, "enabled = %v\n", cfg.Core.Enabled)

	return b.String(), nil
}
