// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"nimblegate/internal/config"
	"nimblegate/internal/paths"
	"nimblegate/internal/selection"
	"nimblegate/internal/stdlib"
	"nimblegate/internal/version"
)

// doctorCheck is one check the doctor runs. Status is "OK", "SKIP",
// "FAIL". Reason is a one-line human explanation; required for non-OK
// statuses, optional for OK.
type doctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"` // OK | SKIP | FAIL
	Reason string `json:"reason,omitempty"`
}

// doctorReport is the full output: the list of checks plus a derived
// overall status (OK / FAIL). SKIPs are informational only - running
// outside a project is normal and should still produce OK.
type doctorReport struct {
	Version string        `json:"version"`
	Checks  []doctorCheck `json:"checks"`
	Status  string        `json:"status"` // OK | FAIL
}

// Doctor implements `nimblegate doctor` - quick health check that
// confirms nimblegate is internally functional. Useful when something
// upstream is misbehaving (CI stuck, harness classifier down, build
// tool hanging) and the user wants to confirm nimblegate isn't the
// source of the problem.
//
// Added 2026-05-21 with Phase 1 Slice 10.
func Doctor(args []string) int {
	fs := flag.NewFlagSet("doctor", flag.ExitOnError)
	asJSON := fs.Bool("json", false, "emit JSON output")
	quick := fs.Bool("quick", false, "skip the selection-sanity check (faster; useful in CI)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	report := doctorReport{Version: version.Resolved()}

	report.Checks = append(report.Checks, checkBinary())
	report.Checks = append(report.Checks, checkStdlib())
	report.Checks = append(report.Checks, checkPatternLink())
	report.Checks = append(report.Checks, checkProject())
	report.Checks = append(report.Checks, checkAuditLog())
	if *quick {
		report.Checks = append(report.Checks, doctorCheck{
			Name:   "selection-sanity",
			Status: "SKIP",
			Reason: "skipped via --quick",
		})
	} else {
		report.Checks = append(report.Checks, checkSelectionSanity())
	}

	report.Status = aggregateStatus(report.Checks)

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
		return exitCodeForStatus(report.Status)
	}

	fmt.Printf("nimblegate doctor: version %s\n\n", report.Version)
	for _, c := range report.Checks {
		fmt.Printf("[%-4s] %-18s %s\n", c.Status, c.Name, c.Reason)
	}
	fmt.Printf("\nStatus: %s\n", report.Status)
	return exitCodeForStatus(report.Status)
}

// aggregateStatus collapses the per-check statuses into a single answer.
// SKIPs are informational only - running outside a project shouldn't
// signal "nimblegate is broken." Only FAIL drives overall FAIL.
func aggregateStatus(checks []doctorCheck) string {
	for _, c := range checks {
		if c.Status == "FAIL" {
			return "FAIL"
		}
	}
	return "OK"
}

func exitCodeForStatus(s string) int {
	if s == "OK" {
		return 0
	}
	return 2
}

// checkBinary confirms the binary identifies itself with a version
// string. Trivially true if doctor ran at all, but documenting the
// version is useful for bug reports.
func checkBinary() doctorCheck {
	v := version.Resolved()
	if v == "" {
		v = "(unset)"
	}
	return doctorCheck{Name: "binary", Status: "OK", Reason: "version " + v}
}

// checkStdlib confirms the embedded frame + pattern catalogs load
// without error and return a non-empty set. A FAIL here means the
// binary is corrupted (very rare) or a malformed frame slipped past
// the test suite (more rare).
func checkStdlib() doctorCheck {
	frames, err := stdlib.Load()
	if err != nil {
		return doctorCheck{Name: "stdlib", Status: "FAIL", Reason: "load frames: " + err.Error()}
	}
	patterns, err := stdlib.LoadPatterns()
	if err != nil {
		return doctorCheck{Name: "stdlib", Status: "FAIL", Reason: "load patterns: " + err.Error()}
	}
	if len(frames) == 0 {
		return doctorCheck{Name: "stdlib", Status: "FAIL", Reason: "0 frames loaded"}
	}
	return doctorCheck{
		Name:   "stdlib",
		Status: "OK",
		Reason: fmt.Sprintf("%d frames, %d patterns", len(frames), len(patterns)),
	}
}

// checkPatternLink confirms every frame's `pattern:` field references
// an existing pattern. Catches frame frontmatter typos that the lint
// pass might miss.
func checkPatternLink() doctorCheck {
	frames, err := stdlib.Load()
	if err != nil {
		return doctorCheck{Name: "pattern-link", Status: "SKIP", Reason: "stdlib load failed (see stdlib check)"}
	}
	patterns, err := stdlib.LoadPatterns()
	if err != nil {
		return doctorCheck{Name: "pattern-link", Status: "SKIP", Reason: "patterns load failed"}
	}
	known := map[string]bool{}
	for _, p := range patterns {
		known[p.ID()] = true
	}
	for _, f := range frames {
		p := f.Frontmatter.Pattern
		if p == "" {
			continue
		}
		if !known[p] {
			return doctorCheck{
				Name:   "pattern-link",
				Status: "FAIL",
				Reason: fmt.Sprintf("frame %s references unknown pattern %q", f.ID(), p),
			}
		}
	}
	return doctorCheck{Name: "pattern-link", Status: "OK", Reason: "all frames link to existing patterns"}
}

// checkProject confirms the project root is detectable + appframes.toml
// parses if present. SKIP when run outside a nimblegate project (e.g.,
// fresh install).
func checkProject() doctorCheck {
	cwd, err := os.Getwd()
	if err != nil {
		return doctorCheck{Name: "project", Status: "FAIL", Reason: "getwd: " + err.Error()}
	}
	root, err := paths.FindProjectRoot(cwd)
	if err != nil {
		return doctorCheck{Name: "project", Status: "SKIP", Reason: "not inside a nimblegate project"}
	}
	cfgPath := paths.ConfigPath(root)
	if _, err := os.Stat(cfgPath); err != nil {
		return doctorCheck{Name: "project", Status: "SKIP", Reason: "appframes.toml not present at " + cfgPath}
	}
	if _, err := config.LoadProject(cfgPath); err != nil {
		return doctorCheck{Name: "project", Status: "FAIL", Reason: "parse appframes.toml: " + err.Error()}
	}
	return doctorCheck{Name: "project", Status: "OK", Reason: "appframes.toml parses; root " + root}
}

// checkAuditLog verifies the audit-log directory is writable by
// performing a tiny write + cleanup. SKIP outside a project (no audit
// directory expected).
func checkAuditLog() doctorCheck {
	cwd, err := os.Getwd()
	if err != nil {
		return doctorCheck{Name: "audit-log", Status: "SKIP", Reason: "getwd failed"}
	}
	root, err := paths.FindProjectRoot(cwd)
	if err != nil {
		return doctorCheck{Name: "audit-log", Status: "SKIP", Reason: "not inside a nimblegate project"}
	}
	auditDir := filepath.Join(root, ".appframes", "audit.parts")
	if err := os.MkdirAll(auditDir, 0o755); err != nil {
		return doctorCheck{Name: "audit-log", Status: "FAIL", Reason: "mkdir " + auditDir + ": " + err.Error()}
	}
	probe := filepath.Join(auditDir, ".doctor-probe")
	if err := os.WriteFile(probe, []byte("ok"), 0o644); err != nil {
		return doctorCheck{Name: "audit-log", Status: "FAIL", Reason: "write probe: " + err.Error()}
	}
	_ = os.Remove(probe)
	return doctorCheck{Name: "audit-log", Status: "OK", Reason: auditDir + " writable"}
}

// checkSelectionSanity runs the negative-selection test for a known-
// reliable frame (documentation/dated-todo) end-to-end. Catches regressions
// in the runner / embed FS / testdata pipeline. Takes ~50-100ms; skip
// via --quick.
func checkSelectionSanity() doctorCheck {
	const sanityFrame = "documentation/dated-todo"
	testdataFS, ok := stdlib.TestdataFS(sanityFrame)
	if !ok {
		return doctorCheck{Name: "selection-sanity", Status: "SKIP", Reason: "no testdata for " + sanityFrame}
	}
	runFS, ok := testdataFS.(selection.FS)
	if !ok {
		return doctorCheck{Name: "selection-sanity", Status: "FAIL", Reason: "testdata fs does not implement selection.FS"}
	}
	checkFns := BuiltinCheckFuncs()
	fn, ok := checkFns[sanityFrame]
	if !ok {
		return doctorCheck{Name: "selection-sanity", Status: "FAIL", Reason: "no CheckFunc bound to " + sanityFrame}
	}
	start := time.Now()
	result, err := selection.Run(sanityFrame, fn, runFS)
	if err != nil {
		return doctorCheck{Name: "selection-sanity", Status: "FAIL", Reason: "run: " + err.Error()}
	}
	if result.Grade != "passing" {
		return doctorCheck{
			Name:   "selection-sanity",
			Status: "FAIL",
			Reason: fmt.Sprintf("%s grade=%s (expected passing), positives %d/%d, negatives %d/%d",
				sanityFrame, result.Grade,
				result.PositivesPassed, result.PositivesTotal,
				result.NegativesPassed, result.NegativesTotal),
		}
	}
	return doctorCheck{
		Name:   "selection-sanity",
		Status: "OK",
		Reason: fmt.Sprintf("%s passing %d/%d+%d/%d (%dms)",
			sanityFrame,
			result.PositivesPassed, result.PositivesTotal,
			result.NegativesPassed, result.NegativesTotal,
			time.Since(start).Milliseconds()),
	}
}
