// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"fmt"
	"net/http"

	"nimblegate/internal/gateway"
)

// feedExportHandler streams the current filtered feed view as a download.
// Honors the same filters as /feed (repo, rejects, before). format=jsonl
// (default) is byte-faithful; format=csv is flattened. No Limit cap.
func feedExportHandler(policyRoot string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		format, err := gateway.ParseExportFormat(r.URL.Query().Get("format"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f := filterFromQuery(r, 0) // limit ignored by ExportDecisions
		repoLabel := f.Repo
		if repoLabel == "" {
			repoLabel = "all"
		}
		ext := "jsonl"
		ct := "application/x-ndjson"
		if format == gateway.ExportCSV {
			ext, ct = "csv", "text/csv"
		}
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Content-Disposition",
			fmt.Sprintf(`attachment; filename="feed-export-%s.%s"`, sanitizeFilename(repoLabel), ext))
		// Stream; ExportDecisions writes directly to w. An error mid-stream
		// can't change already-sent headers - best-effort, operator re-runs.
		_ = gateway.ExportDecisions(policyRoot, f, format, w)
	}
}

// sanitizeFilename keeps the Content-Disposition filename header safe: repo
// names are validated elsewhere, but defend the header regardless.
func sanitizeFilename(s string) string {
	out := make([]rune, 0, len(s))
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_', c == '.':
			out = append(out, c)
		default:
			out = append(out, '_')
		}
	}
	return string(out)
}
