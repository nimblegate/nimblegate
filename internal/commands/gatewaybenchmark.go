// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"

	"nimblegate/internal/benchmark"
	"nimblegate/internal/gateway/analytics"
	"nimblegate/internal/gateway/roi"
)

// gatewayBenchmark implements `nimblegate gateway benchmark score`.
func gatewayBenchmark(args []string) int {
	if len(args) == 0 || args[0] != "score" {
		fmt.Fprintln(os.Stderr, "usage: nimblegate gateway benchmark score --config <file> [--policy-root <dir>] [--json]")
		return 2
	}
	fs := flag.NewFlagSet("gateway benchmark score", flag.ExitOnError)
	cfgPath := fs.String("config", "benchmark.toml", "benchmark config (TOML)")
	policyRoot := fs.String("policy-root", "/etc/nimblegate-gateway/repos", "gateway per-repo config root (holds the audit logs)")
	asJSON := fs.Bool("json", false, "emit the matrix as JSON instead of a table")
	_ = fs.Parse(args[1:])

	cfg, err := benchmark.LoadConfig(*cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	// Validate scored frames against the real stdlib registry: a typo'd scored
	// frame would silently score nothing (under-counting), so fail loudly.
	known := roi.StdlibFrameByID()
	for _, f := range cfg.Scored.Frames {
		if _, ok := known[f]; !ok {
			fmt.Fprintf(os.Stderr, "error: scored frame %q is not a known stdlib frame (typo?)\n", f)
			return 1
		}
	}
	db, err := analytics.Open(analyticsDBPath(*policyRoot))
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	defer db.Close()
	if _, err := analytics.Ingest(db, *policyRoot); err != nil {
		fmt.Fprintln(os.Stderr, "error: ingest:", err)
		return 1
	}
	m, err := benchmark.Score(db, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}

	if *asJSON {
		b, _ := json.MarshalIndent(m, "", "  ")
		fmt.Println(string(b))
		return 0
	}
	renderMatrixTable(m)
	return 0
}

// renderMatrixTable prints the matrix grouped by stack, agents as rows.
func renderMatrixTable(m benchmark.Matrix) {
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	curStack := ""
	for _, c := range m.Cells {
		if c.Stack != curStack {
			if curStack != "" {
				fmt.Fprintln(w)
			}
			curStack = c.Stack
			fmt.Fprintf(w, "stack: %s\n", c.Stack)
			fmt.Fprintln(w, "agent\truns\tclean/push\tconverged\tconv@\trecurrence")
		}
		fmt.Fprintf(w, "%s\t%d\t%.2f±%.2f\t%.0f%%\t%.1f±%.1f\t%.2f±%.2f\n",
			c.Agent, c.Runs,
			c.Cleanliness.Mean, c.Cleanliness.StdDev,
			c.ConvergedRate*100,
			c.Convergence.Mean, c.Convergence.StdDev,
			c.Recurrence.Mean, c.Recurrence.StdDev)
	}
	w.Flush()
}
