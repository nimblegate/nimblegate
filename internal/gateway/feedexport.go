// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ExportFormat selects the serialization of a feed export.
type ExportFormat string

const (
	ExportJSONL ExportFormat = "jsonl"
	ExportCSV   ExportFormat = "csv"
)

// ExportDecisions streams every audit record matching f (Repo / RejectsOnly /
// Before) to w. There is NO Limit cap - export is the full filtered set.
// JSONL writes the original on-disk line bytes (byte-faithful). CSV writes a
// header then one flattened row per decision. Read-only; touches nothing.
func ExportDecisions(policyRoot string, f Filter, format ExportFormat, w io.Writer) error {
	matches, _ := filepath.Glob(filepath.Join(policyRoot, "*", "audit.log"))

	var csvW *csv.Writer
	if format == ExportCSV {
		csvW = csv.NewWriter(w)
		if err := csvW.Write([]string{"time", "repo", "refs", "accept", "observed", "finding_count", "messages"}); err != nil {
			return err
		}
	}

	for _, p := range matches {
		f2, err := os.Open(p)
		if err != nil {
			continue // fail-soft, matches ReadDecisions
		}
		sc := bufio.NewScanner(f2)
		sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for sc.Scan() {
			line := sc.Bytes()
			if len(line) == 0 {
				continue
			}
			var rec AuditRecord
			if json.Unmarshal(line, &rec) != nil {
				continue // unrenderable on the read path
			}
			if f.Repo != "" && rec.Repo != f.Repo {
				continue
			}
			if f.RejectsOnly && rec.Accept {
				continue
			}
			if !f.Before.IsZero() && !rec.Time.Before(f.Before) {
				continue
			}
			if format == ExportCSV {
				if err := csvW.Write([]string{
					rec.Time.UTC().Format("2006-01-02T15:04:05Z07:00"),
					rec.Repo,
					strings.Join(rec.Refs, " "),
					strconv.FormatBool(rec.Accept),
					strconv.FormatBool(rec.Observed),
					strconv.Itoa(len(rec.Findings)),
					strings.Join(rec.Messages, " | "),
				}); err != nil {
					f2.Close()
					return err
				}
			} else {
				if _, err := w.Write(line); err != nil {
					f2.Close()
					return err
				}
				if _, err := io.WriteString(w, "\n"); err != nil {
					f2.Close()
					return err
				}
			}
		}
		f2.Close()
	}
	if csvW != nil {
		csvW.Flush()
		return csvW.Error()
	}
	return nil
}

// ParseExportFormat maps a query value to an ExportFormat, defaulting to JSONL.
func ParseExportFormat(s string) (ExportFormat, error) {
	switch s {
	case "", "jsonl":
		return ExportJSONL, nil
	case "csv":
		return ExportCSV, nil
	default:
		return "", fmt.Errorf("unknown export format %q", s)
	}
}
