// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
)

// cfDatasetRetention is the Free-tier retention window for each CF
// Analytics GraphQL dataset. Exceeding the window in a query produces a
// hard rejection from the API - and a confusing error message that
// doesn't point at the dataset choice as the cause.
//
// Source: Cloudflare GraphQL Analytics API docs + a downstream
// project's incident catalog. Numbers reflect the Free tier; paid
// tiers may extend the windows but this check uses the strictest cap
// to flag the highest-risk queries.
var cfDatasetRetention = map[string]time.Duration{
	"httpRequestsAdaptiveGroups":   1 * 24 * time.Hour,  // 1 day on Free
	"httpRequestsAdaptive":         1 * 24 * time.Hour,  // singular form
	"httpRequests1hGroups":         3 * 24 * time.Hour,  // 3 days
	"httpRequests1mGroups":         3 * 24 * time.Hour,  // 3 days
	"httpRequests1dGroups":         30 * 24 * time.Hour, // 30 days
	"firewallEventsAdaptiveGroups": 1 * 24 * time.Hour,  // 1 day
}

// cfDatasetRegex matches an invocation of one of the known datasets in
// a GraphQL query. We anchor on the dataset name appearing after `viewer`
// or directly at the start of a selection - both shapes appear in real
// CF GraphQL queries.
var cfDatasetRegex = regexp.MustCompile(`\b(httpRequestsAdaptiveGroups|httpRequestsAdaptive|httpRequests1hGroups|httpRequests1mGroups|httpRequests1dGroups|firewallEventsAdaptiveGroups)\b`)

// cfDatetimeRegex extracts `datetime_geq` / `date_geq` / `datetimeFromTo`
// argument values. Captures ISO-8601 timestamps and YYYY-MM-DD dates.
// Different dataset families use different argument names - we accept
// any of them.
var cfDatetimeRegex = regexp.MustCompile(`(?:datetime_geq|date_geq|datetime_leq|date_leq)\s*:\s*"([^"]+)"`)

// cfGraphQLApplicableFile returns true for files that look like they
// host CF GraphQL queries: n8n workflow JSON, raw .graphql files, and
// .gql files under infra/ directories.
func cfGraphQLApplicableFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".graphql", ".gql":
		return true
	}
	// n8n workflow JSON files conventionally live under infra/n8n/workflows/.
	if ext == ".json" {
		parts := strings.Split(filepath.ToSlash(path), "/")
		for i, p := range parts {
			pl := strings.ToLower(p)
			if pl == "n8n" || pl == "workflows" {
				// Only count if it's deep enough - avoids matching every JSON
				// in a `workflows/` directory in unrelated repos.
				if i+1 < len(parts) {
					return true
				}
			}
		}
	}
	return false
}

const cfGraphQLDisableMarker = "appframes:disable app-correctness/cf-graphql-dataset-by-window"
const cfGraphQLDisableLineMarker = "appframes:disable-next-line app-correctness/cf-graphql-dataset-by-window"
const cfGraphQLMaxFileBytes = 1 << 20 // 1 MiB

// CFGraphQLDatasetByWindow scans GraphQL queries targeting Cloudflare
// Analytics for time-window vs dataset-retention mismatches. The same
// 7-day query rejected by `httpRequestsAdaptiveGroups` (1d cap) will
// succeed against `httpRequests1dGroups` (30d retention) - but each
// dataset has a different schema, so the fix isn't just a name swap.
//
// Reference: AGENTS_LEARNING §15. Three failed redeploys before landing
// on the right dataset.
func CFGraphQLDatasetByWindow(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "app-correctness/cf-graphql-dataset-by-window",
		Category: frames.CategoryAppCorrectness,
	}

	files := ctx.ChangedFiles
	if len(files) == 0 && ctx.Trigger == engine.TriggerCLI {
		_ = filepath.WalkDir(ctx.ProjectRoot, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if ShouldSkipPath(ctx, path) {
					return filepath.SkipDir
				}
				return nil
			}
			if cfGraphQLApplicableFile(path) {
				files = append(files, path)
			}
			return nil
		})
	}

	var hits []string
	var hitsStruct []engine.Hit
	const hitCap = 10

filesLoop:
	for _, file := range files {
		if !cfGraphQLApplicableFile(file) {
			continue
		}
		if ShouldSkipPath(ctx, file) {
			continue
		}
		info, err := os.Stat(file)
		if err != nil || info.Size() > cfGraphQLMaxFileBytes {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		content := string(data)
		if strings.Contains(content, cfGraphQLDisableMarker) {
			continue
		}

		// Find every dataset reference. For each, scan the surrounding
		// lines for datetime args and check the span.
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			if i > 0 && strings.Contains(lines[i-1], cfGraphQLDisableLineMarker) {
				continue
			}
			m := cfDatasetRegex.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			dataset := m[1]
			cap, ok := cfDatasetRetention[dataset]
			if !ok {
				continue
			}
			// Look at this line plus the next 30 lines for the datetime args.
			// (CF GraphQL queries put the dataset and filter on adjacent
			// lines in every real-world example we've seen.)
			windowSearch := lines[i:]
			if len(windowSearch) > 30 {
				windowSearch = windowSearch[:30]
			}
			span, ok := cfQueryTimeSpan(strings.Join(windowSearch, "\n"))
			if !ok {
				continue
			}
			if span <= cap {
				continue
			}
			label := fmt.Sprintf("%s (cap %s) queried over %s - exceeds retention",
				dataset, friendlyDuration(cap), friendlyDuration(span))
			hits = append(hits, fmt.Sprintf("%s:%d - %s", file, i+1, label))
			hitsStruct = append(hitsStruct, engine.Hit{File: file, Line: i + 1, Label: label})
			if len(hits) >= hitCap {
				break filesLoop
			}
		}
	}

	if len(hits) == 0 {
		res.Outcome = engine.OutcomePass
		return res
	}
	res.Hits = hitsStruct
	res.Outcome = engine.OutcomeBlock
	res.Reason = "CF GraphQL queries with time windows exceeding the dataset's retention: " + strings.Join(hits, "; ")
	res.Fix = "switch to a dataset whose retention covers the requested window: httpRequestsAdaptiveGroups → 1d, httpRequests1hGroups → 3d, httpRequests1dGroups → 30d. Each has a different schema - `count` on adaptive, `sum.requests` on 1h/1d - adjust the query body accordingly."
	return res
}

// cfQueryTimeSpan extracts a (from, to) pair from the datetime arguments
// in a GraphQL query body and returns to-from. Returns (0, false) when it
// can't find a clean span.
func cfQueryTimeSpan(body string) (time.Duration, bool) {
	matches := cfDatetimeRegex.FindAllStringSubmatch(body, -1)
	if len(matches) < 2 {
		return 0, false
	}
	// Parse all timestamps we found; the span is max - min.
	var times []time.Time
	for _, m := range matches {
		t, ok := parseCFDatetime(m[1])
		if !ok {
			continue
		}
		times = append(times, t)
	}
	if len(times) < 2 {
		return 0, false
	}
	min, max := times[0], times[0]
	for _, t := range times[1:] {
		if t.Before(min) {
			min = t
		}
		if t.After(max) {
			max = t
		}
	}
	return max.Sub(min), true
}

// parseCFDatetime accepts both ISO-8601 timestamps and YYYY-MM-DD dates
// (the two formats different CF datasets use). Returns (zero, false)
// for anything else.
func parseCFDatetime(s string) (time.Time, bool) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, true
	}
	if t, err := time.Parse("2006-01-02T15:04:05Z", s); err == nil {
		return t, true
	}
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t, true
	}
	return time.Time{}, false
}

// friendlyDuration returns a short human label ("1d", "3d", "30d", "12h").
func friendlyDuration(d time.Duration) string {
	days := int(d / (24 * time.Hour))
	if days >= 1 && d%(24*time.Hour) == 0 {
		return fmt.Sprintf("%dd", days)
	}
	hours := int(d / time.Hour)
	if hours >= 1 {
		return fmt.Sprintf("%dh", hours)
	}
	return d.String()
}
