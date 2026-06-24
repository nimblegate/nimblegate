// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
)

// cfDatasetSchema describes which top-level fields the CF GraphQL API
// exposes on each dataset. The schema differs by dataset family:
//
//   - Adaptive datasets expose `count` (and `dimensions`/`avg`).
//   - 1h / 1m / 1d grouped datasets expose `sum.{requests,bytes,...}`
//     and `uniq.uniques` but NOT `count`.
//
// The field-set lists are NOT exhaustive - we list the high-frequency
// fields that real CF GraphQL queries use. The check is "this field
// can NEVER appear on this dataset," not "every field is in the list."
type cfDatasetSchema struct {
	ValidTopLevelFields map[string]bool
	InvalidTopFields    map[string]string // field name → human reason
}

var cfDatasetSchemas = map[string]cfDatasetSchema{
	"httpRequestsAdaptiveGroups": {
		ValidTopLevelFields: map[string]bool{
			"count": true, "dimensions": true, "avg": true,
		},
		InvalidTopFields: map[string]string{
			"sum": "Adaptive datasets do not expose `sum`; use `count` plus filters, or switch to httpRequests1hGroups/1dGroups for aggregated counts",
		},
	},
	"httpRequestsAdaptive": {
		ValidTopLevelFields: map[string]bool{
			"count": true, "dimensions": true, "avg": true,
		},
		InvalidTopFields: map[string]string{
			"sum": "Adaptive datasets do not expose `sum`; use `count`",
		},
	},
	"httpRequests1hGroups": {
		ValidTopLevelFields: map[string]bool{
			"sum": true, "uniq": true, "dimensions": true, "avg": true,
		},
		InvalidTopFields: map[string]string{
			"count": "Grouped datasets do not expose top-level `count`; use `sum.requests` (or another sum.X)",
		},
	},
	"httpRequests1mGroups": {
		ValidTopLevelFields: map[string]bool{
			"sum": true, "uniq": true, "dimensions": true, "avg": true,
		},
		InvalidTopFields: map[string]string{
			"count": "Grouped datasets do not expose top-level `count`; use `sum.requests`",
		},
	},
	"httpRequests1dGroups": {
		ValidTopLevelFields: map[string]bool{
			"sum": true, "uniq": true, "dimensions": true, "avg": true,
		},
		InvalidTopFields: map[string]string{
			"count": "Grouped datasets do not expose top-level `count`; use `sum.requests`",
		},
	},
}

// cfQueryBlockRegex extracts a dataset call body. Matches `<dataset>(<args>) { <body> }`
// non-greedily so adjacent datasets don't bleed into each other.
var cfQueryBlockRegex = regexp.MustCompile(`(?s)\b(httpRequestsAdaptiveGroups|httpRequestsAdaptive|httpRequests1hGroups|httpRequests1mGroups|httpRequests1dGroups)\s*\([^)]*\)\s*\{(.+?)\}`)

// cfTopLevelFieldRegex extracts field names from a query selection.
// We look for words at the start of a line (after whitespace), without
// a `:` (to avoid matching arg names). The non-greedy block extraction
// gives us only the dataset's body.
var cfTopLevelFieldRegex = regexp.MustCompile(`(?m)^\s*([a-zA-Z_][a-zA-Z0-9_]*)(?:\s|$|\{)`)

const cfSchemaMatchDisableMarker = "appframes:disable app-correctness/cf-graphql-schema-match"
const cfSchemaMatchDisableLineMarker = "appframes:disable-next-line app-correctness/cf-graphql-schema-match"
const cfSchemaMatchMaxFileBytes = 1 << 20 // 1 MiB

// CFGraphQLSchemaMatch scans CF GraphQL queries for dataset/field
// mismatches: `count` queried on a grouped dataset, `sum.X` on an
// adaptive dataset, etc. Each rejection from the live API comes back
// as a generic error that doesn't point at the schema - surfacing it
// at lint time saves the round trip.
//
// Companion to `app-correctness/cf-graphql-dataset-by-window`: that
// frame catches "wrong dataset for the time window"; this one catches
// "right dataset but wrong fields."
//
// Reference incident: AGENTS_LEARNING §15 / cf-incidents §5 (frame
// proposal #12). Three failed redeploys before landing on the right
// combination of dataset and schema.
func CFGraphQLSchemaMatch(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "app-correctness/cf-graphql-schema-match",
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
		if err != nil || info.Size() > cfSchemaMatchMaxFileBytes {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		content := string(data)
		if strings.Contains(content, cfSchemaMatchDisableMarker) {
			continue
		}

		// For every dataset query block, extract the top-level fields and
		// check them against the dataset's schema.
		for _, m := range cfQueryBlockRegex.FindAllStringSubmatchIndex(content, -1) {
			dataset := content[m[2]:m[3]]
			body := content[m[4]:m[5]]
			schema, ok := cfDatasetSchemas[dataset]
			if !ok {
				continue
			}
			// Compute the 1-based line number of the dataset reference.
			datasetStartLine := 1 + strings.Count(content[:m[2]], "\n")

			// Check for line-level disable on the line above the dataset.
			lines := strings.Split(content[:m[2]], "\n")
			if len(lines) >= 2 && strings.Contains(lines[len(lines)-2], cfSchemaMatchDisableLineMarker) {
				continue
			}

			for _, fm := range cfTopLevelFieldRegex.FindAllStringSubmatch(body, -1) {
				field := fm[1]
				if reason, bad := schema.InvalidTopFields[field]; bad {
					label := fmt.Sprintf("%s: field `%s` is invalid - %s", dataset, field, reason)
					hits = append(hits, fmt.Sprintf("%s:%d - %s", file, datasetStartLine, label))
					hitsStruct = append(hitsStruct, engine.Hit{File: file, Line: datasetStartLine, Label: label})
					if len(hits) >= hitCap {
						break filesLoop
					}
				}
			}
		}
	}

	if len(hits) == 0 {
		res.Outcome = engine.OutcomePass
		return res
	}
	res.Hits = hitsStruct
	res.Outcome = engine.OutcomeBlock
	res.Reason = "CF GraphQL queries with field/dataset mismatches: " + strings.Join(hits, "; ")
	res.Fix = "match the field to the dataset family: Adaptive datasets use `count`; 1h/1m/1d grouped datasets use `sum.<field>` (e.g. `sum { requests }`). When in doubt, run a quick query against the CF GraphQL Explorer to see the schema for the dataset before committing."
	return res
}
