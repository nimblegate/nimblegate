// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"nimblegate/internal/engine"
	"nimblegate/internal/paths"
)

// Audit dispatches `nimblegate audit <subcommand>`.
//
//	nimblegate audit reset [--backup] [--yes]
//	    Clears the project's audit log. With --backup, renames
//	    audit.log → audit.log.reset-YYYYMMDD-HHMMSS first; rotated
//	    siblings (audit.log.1, .2, ...) are also moved or removed.
//	    Without --backup the log is destroyed and --yes is required
//	    to confirm.
//
//	nimblegate audit analyze [--window 30d] [--frame ID] [--json]
//	    Surface patterns in the audit log: top-bypassed frames, reason
//	    hotspots, stale frames, and the estimated time prevented.
//
//	nimblegate audit compact [--quiescence DURATION]
//	    Merge quiescent per-process part files under .appframes/audit.parts/
//	    into the consolidated audit.log. Opportunistic - also runs
//	    automatically when status / analyze fire.
//
// Returns the desired process exit code.
func Audit(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "nimblegate audit: subcommand required (reset, analyze, compact)")
		return 2
	}
	sub := args[0]
	rest := args[1:]

	switch sub {
	case "reset":
		return auditReset(rest)
	case "analyze":
		return auditAnalyze(rest)
	case "compact":
		return auditCompact(rest)
	case "--help", "-h", "help":
		fmt.Println("nimblegate audit: audit log management + retrospective analysis")
		fmt.Println()
		fmt.Println("Usage: nimblegate audit <subcommand>")
		fmt.Println()
		fmt.Println("Subcommands:")
		fmt.Println("  analyze [--window 30d] [--frame ID] [--json]  Top-bypassed frames + reason clusters +")
		fmt.Println("                                                stale frames + time-prevented estimates")
		fmt.Println("  compact [--quiescence DURATION]               Merge audit.parts/ files into audit.log")
		fmt.Println("  reset [--backup] [--yes]                      Reset the project audit log (destructive)")
		return 0
	default:
		fmt.Fprintf(os.Stderr, "nimblegate audit: unknown subcommand %q (use reset | analyze | compact; --help for usage)\n", sub)
		return 2
	}
}

// auditCompact runs CompactAudit and reports the result.
func auditCompact(args []string) int {
	fs := flag.NewFlagSet("audit compact", flag.ExitOnError)
	quiescenceFlag := fs.String("quiescence", "5m", "minimum age of a part file before it's eligible (e.g. 5m, 1h)")
	_ = fs.Parse(args)

	dur, err := parseSinceDuration(*quiescenceFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate audit compact: invalid --quiescence %q: %v\n", *quiescenceFlag, err)
		return 2
	}

	cwd, err := os.Getwd()
	if err != nil {
		return 2
	}
	root, err := paths.FindProjectRoot(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate audit compact: %v\n", err)
		return 2
	}
	result, err := engine.CompactAudit(root, dur)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate audit compact: %v\n", err)
		return 2
	}
	fmt.Printf("✓ considered %d part(s); consumed %d; appended %d byte(s)\n",
		result.PartsConsidered, result.PartsConsumed, result.BytesAppended)
	for _, s := range result.Skipped {
		fmt.Fprintf(os.Stderr, "  skipped: %s\n", s)
	}
	return 0
}

// auditReset implements `audit reset`. Returns the process exit code.
func auditReset(args []string) int {
	fs := flag.NewFlagSet("audit reset", flag.ExitOnError)
	backup := fs.Bool("backup", false, "preserve current audit log family as audit.log.reset-<timestamp>[.N]")
	yes := fs.Bool("yes", false, "skip confirmation when destroying without --backup")
	_ = fs.Parse(args)

	cwd, err := os.Getwd()
	if err != nil {
		return 2
	}
	root, err := paths.FindProjectRoot(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate audit reset: %v\n", err)
		return 2
	}
	logPath := paths.AuditLogPath(root)
	rotated := engine.RotatedFiles(logPath)

	// Filter to files that actually exist on disk (RotatedFiles returns
	// current even when missing).
	var existing []string
	for _, p := range rotated {
		if _, err := os.Stat(p); err == nil {
			existing = append(existing, p)
		}
	}
	if len(existing) == 0 {
		fmt.Println("(no audit log to reset)")
		return 0
	}

	if !*backup && !*yes {
		fmt.Fprintln(os.Stderr, "nimblegate audit reset: refusing to destroy audit log without --backup or --yes.")
		fmt.Fprintf(os.Stderr, "  Files that would be removed:\n")
		for _, p := range existing {
			fmt.Fprintf(os.Stderr, "    %s\n", p)
		}
		fmt.Fprintln(os.Stderr, "  Re-run with --backup to preserve them, or --yes to confirm deletion.")
		return 1
	}

	if *backup {
		stamp := time.Now().UTC().Format("20060102-150405")
		for _, p := range existing {
			// audit.log → audit.log.reset-<stamp>
			// audit.log.1 → audit.log.reset-<stamp>.1 (so the family stays grouped)
			suffix := ""
			base := filepath.Base(logPath)
			if filepath.Base(p) != base {
				suffix = "." + filepath.Base(p)[len(base)+1:]
			}
			newPath := logPath + ".reset-" + stamp + suffix
			if err := os.Rename(p, newPath); err != nil {
				fmt.Fprintf(os.Stderr, "nimblegate audit reset: rename %s → %s: %v\n", p, newPath, err)
				return 2
			}
		}
		fmt.Printf("✓ Audit log preserved as %s.reset-%s[.N]; new audit.log will be created on next check.\n",
			filepath.Base(logPath), stamp)
		return 0
	}

	// Destructive path: --yes confirmed.
	for _, p := range existing {
		if err := os.Remove(p); err != nil {
			fmt.Fprintf(os.Stderr, "nimblegate audit reset: remove %s: %v\n", p, err)
			return 2
		}
	}
	fmt.Printf("✓ Removed %d audit-log file(s).\n", len(existing))
	return 0
}
