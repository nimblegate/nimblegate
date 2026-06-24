// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"nimblegate/internal/gateway/analytics"
)

func gatewayAnalytics(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: nimblegate gateway analytics <ingest|stats> [flags]")
		return 2
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "ingest":
		return analyticsIngest(rest)
	case "stats":
		return analyticsStats(rest)
	default:
		fmt.Fprintf(os.Stderr, "unknown analytics subcommand %q (want ingest|stats)\n", sub)
		return 2
	}
}

func analyticsDBPath(policyRoot string) string {
	return filepath.Join(policyRoot, "analytics.db")
}

func analyticsIngest(args []string) int {
	fs := flag.NewFlagSet("gateway analytics ingest", flag.ExitOnError)
	policyRoot := fs.String("policy-root", "/etc/nimblegate-gateway/repos", "gateway per-repo config root")
	_ = fs.Parse(args)

	db, err := analytics.Open(analyticsDBPath(*policyRoot))
	if err != nil {
		fmt.Fprintf(os.Stderr, "analytics: open db: %v\n", err)
		return 1
	}
	defer db.Close()
	res, err := analytics.Ingest(db, *policyRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "analytics: ingest: %v\n", err)
		return 1
	}
	fmt.Printf("ingested %d new decision(s), skipped %d malformed line(s)\n", res.Inserted, res.Skipped)
	return 0
}

func analyticsStats(args []string) int {
	fs := flag.NewFlagSet("gateway analytics stats", flag.ExitOnError)
	policyRoot := fs.String("policy-root", "/etc/nimblegate-gateway/repos", "gateway per-repo config root")
	repo := fs.String("repo", "", "filter to one repo")
	since := fs.String("since", "", "lower time bound (RFC3339 or duration like 720h)")
	until := fs.String("until", "", "upper time bound (RFC3339)")
	asJSON := fs.Bool("json", false, "emit JSON")
	_ = fs.Parse(args)

	q := analytics.StatsQuery{Repo: *repo}
	if *since != "" {
		ts, err := parseTimeBound(*since)
		if err != nil {
			fmt.Fprintf(os.Stderr, "analytics: bad --since: %v\n", err)
			return 2
		}
		q.Since = ts
	}
	if *until != "" {
		ts, err := parseTimeBound(*until)
		if err != nil {
			fmt.Fprintf(os.Stderr, "analytics: bad --until: %v\n", err)
			return 2
		}
		q.Until = ts
	}

	db, err := analytics.Open(analyticsDBPath(*policyRoot))
	if err != nil {
		fmt.Fprintf(os.Stderr, "analytics: open db: %v\n", err)
		return 1
	}
	defer db.Close()
	if _, err := analytics.Ingest(db, *policyRoot); err != nil {
		fmt.Fprintf(os.Stderr, "analytics: ingest: %v\n", err)
		return 1
	}
	s, err := analytics.Stats(db, q)
	if err != nil {
		fmt.Fprintf(os.Stderr, "analytics: stats: %v\n", err)
		return 1
	}

	if *asJSON {
		b, _ := json.MarshalIndent(s, "", "  ")
		fmt.Println(string(b))
		return 0
	}
	printStatsText(s)
	return 0
}

// parseTimeBound accepts an RFC3339 timestamp or a Go duration (interpreted as
// "now minus duration", e.g. 720h → 30 days ago).
func parseTimeBound(v string) (time.Time, error) {
	if ts, err := time.Parse(time.RFC3339, v); err == nil {
		return ts, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return time.Time{}, fmt.Errorf("not RFC3339 or a duration: %q", v)
	}
	return time.Now().Add(-d), nil
}

func printStatsText(s analytics.StatsResult) {
	if !s.Consistent {
		fmt.Printf("WARNING: counts don't add up: %d decisions != %d accepts + %d rejects (possible data glitch)\n",
			s.Decisions, s.Accepts, s.Rejects)
	}
	fmt.Printf("decisions: %d  accepts: %d  rejects: %d  repos: %d\n",
		s.Decisions, s.Accepts, s.Rejects, s.Repos)
	if len(s.PerRepo) > 0 {
		fmt.Println("\nper repo:")
		for _, r := range s.PerRepo {
			fmt.Printf("  %-30s decisions=%-6d rejects=%d\n", r.Repo, r.Decisions, r.Rejects)
		}
	}
	if len(s.TopFrames) > 0 {
		fmt.Println("\ntop frames:")
		for _, f := range s.TopFrames {
			fmt.Printf("  %-5s %-40s %d\n", f.Severity, f.FrameID, f.Count)
		}
	}
}
