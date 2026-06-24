// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"nimblegate/internal/kits"
)

func Kits(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: nimblegate kits {list|apply|clear|create|delete|status} [args]")
		return 2
	}

	// Dispatch to v2 implementations when the operator's appframes.toml is
	// schema v2. The v2 catalog has different semantics (axis-selections vs
	// frame-lists), so the subcommand handlers diverge entirely. v1 path
	// preserved for backwards-compat.
	v2Mode := isV2Config()

	switch args[0] {
	case "list":
		if v2Mode {
			return kitsV2List(args[1:])
		}
		return kitsList(args[1:])
	case "apply":
		if v2Mode {
			return kitsV2Apply(args[1:])
		}
		return kitsApply(args[1:])
	case "status":
		// v2-only: surfaces version delta between applied kits and stdlib.
		return kitsV2Status(args[1:])
	case "clear":
		return kitsClear(args[1:])
	case "create":
		return kitsCreate(args[1:])
	case "delete":
		return kitsDelete(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "nimblegate kits: unknown subcommand %q\n", args[0])
		return 2
	}
}

// isV2Config detects whether the operator's appframes.toml is schema v2.
// Returns false on missing file, parse errors, or v1 schema (the common
// case for projects that haven't migrated).
func isV2Config() bool {
	cwd, err := os.Getwd()
	if err != nil {
		return false
	}
	for d := cwd; d != "/" && d != ""; d = filepath.Dir(d) {
		cfgPath := filepath.Join(d, "appframes.toml")
		if _, err := os.Stat(cfgPath); err == nil {
			version, _ := probeSchemaVersion(cfgPath)
			return version == 2
		}
	}
	return false
}

func kitsList(args []string) int {
	flags := flag.NewFlagSet("kits list", flag.ContinueOnError)
	jsonOut := flags.Bool("json", false, "JSON output")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	ks, err := kits.LoadStdlib()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	cfgPath := "appframes.toml"
	applied := map[string]bool{}
	if cfg, _ := readFullConfig(cfgPath); cfg != nil {
		for _, n := range cfg.UI.AppliedKits {
			applied[n] = true
		}
	}
	type row struct {
		Name        string `json:"name"`
		Display     string `json:"display"`
		Description string `json:"description"`
		Frames      int    `json:"frames"`
		Applied     bool   `json:"applied"`
	}
	var rows []row
	for _, k := range ks.All() {
		rows = append(rows, row{
			Name: k.Name, Display: k.Display, Description: k.Description,
			Frames: len(k.Frames), Applied: applied[k.Name],
		})
	}
	if *jsonOut {
		json.NewEncoder(os.Stdout).Encode(rows)
		return 0
	}
	for _, r := range rows {
		mark := "  "
		if r.Applied {
			mark = "✓ "
		}
		fmt.Printf("%s%-22s %s (%d frames)\n", mark, r.Name, r.Description, r.Frames)
	}
	return 0
}

func kitsApply(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: nimblegate kits apply <name>")
		return 2
	}
	name := args[0]
	ks, _ := kits.LoadStdlib()
	k, ok := ks.Get(name)
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown kit %q. Available: %s\n", name, strings.Join(ks.Names(), ", "))
		return 1
	}
	cfgPath := "appframes.toml"
	if err := applyFramesToConfig(cfgPath, k.Frames); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := addAppliedKit(cfgPath, name); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("Applied kit %q (%d frames added).\n", name, len(k.Frames))
	return 0
}

func kitsClear(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: nimblegate kits clear <name>")
		return 2
	}
	name := args[0]
	ks, _ := kits.LoadStdlib()
	k, ok := ks.Get(name)
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown kit %q\n", name)
		return 1
	}
	cfgPath := "appframes.toml"
	if err := clearFramesFromConfig(cfgPath, k.Frames); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	if err := removeAppliedKit(cfgPath, name); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("Cleared kit %q.\n", name)
	return 0
}

func kitsCreate(args []string) int {
	flags := flag.NewFlagSet("kits create", flag.ContinueOnError)
	framesCSV := flags.String("frames", "", "comma-separated frame IDs")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 1 || *framesCSV == "" {
		fmt.Fprintln(os.Stderr, "usage: nimblegate kits create <name> --frames id1,id2,...")
		return 2
	}
	name := flags.Arg(0)
	ids := strings.Split(*framesCSV, ",")
	cfgPath := "appframes.toml"
	if err := addUserKit(cfgPath, name, ids); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("Created custom kit %q (%d frames).\n", name, len(ids))
	return 0
}

func kitsDelete(args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: nimblegate kits delete <name>")
		return 2
	}
	cfgPath := "appframes.toml"
	if err := deleteUserKit(cfgPath, args[0]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("Deleted custom kit %q (frames not unticked).\n", args[0])
	return 0
}

// applyFramesToConfig ticks each frame ID into [frames] enabled,
// idempotent across already-enabled IDs.
func applyFramesToConfig(cfgPath string, ids []string) error {
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return err
	}
	doc := string(data)
	for _, id := range ids {
		updated, _, err := rewriteEnabledList(doc, id, true)
		if err != nil {
			return err
		}
		doc = updated
	}
	return atomicWriteFile(cfgPath, []byte(doc))
}

// clearFramesFromConfig unticks each frame ID from [frames] enabled,
// idempotent across already-disabled IDs.
func clearFramesFromConfig(cfgPath string, ids []string) error {
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return err
	}
	doc := string(data)
	for _, id := range ids {
		updated, _, err := rewriteEnabledList(doc, id, false)
		if err != nil {
			return err
		}
		doc = updated
	}
	return atomicWriteFile(cfgPath, []byte(doc))
}
