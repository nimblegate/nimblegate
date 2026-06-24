// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	v2 "nimblegate/internal/config/v2"
	v2kits "nimblegate/internal/kits/v2"
	"nimblegate/internal/paths"
)

// kitsV2StatusImpl is the implementation of `nimblegate kits status` for v2
// configs. Compares each applied kit's recorded semver+hash against the
// current stdlib kit to detect available updates.
//
// Per spec §7.5 + decision #18 (hybrid versioning):
//   - semver delta → "Kit X has updates available: vY → vZ"
//   - semver same, hash differs → "Kit X content changed cosmetically; no
//     action needed unless curious"
//   - both match → "Kit X up to date"
//   - kit no longer in stdlib → "Kit X applied but no longer in stdlib"
func kitsV2StatusImpl(args []string) int {
	fs := flag.NewFlagSet("kits status", flag.ContinueOnError)
	verbose := fs.Bool("verbose", false, "include cosmetic hash-only changes in output")
	_ = fs.Parse(args)

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate kits status: getwd: %v\n", err)
		return 2
	}
	root, err := paths.FindProjectRoot(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate kits status: %v\n", err)
		return 2
	}
	cfgPath := filepath.Join(root, "appframes.toml")
	cfg, err := v2.Load(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate kits status: load v2 config: %v\n", err)
		return 2
	}

	if len(cfg.Meta.AppliedKits) == 0 {
		fmt.Println("No kits applied. Run `nimblegate kits list` to see available.")
		return 0
	}

	set, err := v2kits.LoadStdlib()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate kits status: load stdlib: %v\n", err)
		return 2
	}

	hasUpdates := false
	fmt.Println("Applied kits:")
	for _, ak := range cfg.Meta.AppliedKits {
		current, ok := set.Get(ak.ID)
		if !ok {
			fmt.Printf("  ? %s (v%s): kit no longer in stdlib (applied %s)\n", ak.ID, ak.Semver, ak.AppliedAt)
			continue
		}
		curHash := current.Hash()
		switch {
		case current.Semver != ak.Semver:
			fmt.Printf("  ↑ %s: UPDATE AVAILABLE: v%s → v%s\n", ak.ID, ak.Semver, current.Semver)
			fmt.Printf("       run `nimblegate kits apply %s` to apply the updates\n", ak.ID)
			hasUpdates = true
		case curHash != ak.Hash:
			if *verbose {
				fmt.Printf("  ~ %s (v%s): content changed cosmetically since apply (hash differs); no action needed\n", ak.ID, ak.Semver)
			} else {
				fmt.Printf("  ✓ %s (v%s)\n", ak.ID, ak.Semver)
			}
		default:
			fmt.Printf("  ✓ %s (v%s): up to date\n", ak.ID, ak.Semver)
		}
	}

	if hasUpdates {
		fmt.Println()
		fmt.Println("Run `nimblegate kits apply <kit_id>` to re-apply with the updated stdlib version.")
	}
	return 0
}
