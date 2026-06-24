// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"bytes"
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExportDecisions_JSONLByteFaithfulAndFiltered(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "r")
	_ = os.MkdirAll(repo, 0o755)
	acceptLine := `{"time":"2026-06-01T00:00:00Z","repo":"r","accept":true}`
	rejectLine := `{"time":"2026-06-02T00:00:00Z","repo":"r","accept":false}`
	_ = os.WriteFile(filepath.Join(repo, "audit.log"), []byte(acceptLine+"\n"+rejectLine+"\n"), 0o600)

	var buf bytes.Buffer
	if err := ExportDecisions(root, Filter{RejectsOnly: true}, ExportJSONL, &buf); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(buf.String())
	if got != rejectLine { // byte-faithful original line, only the reject
		t.Fatalf("jsonl export mismatch:\n got: %q\nwant: %q", got, rejectLine)
	}
}

func TestExportDecisions_CSVHeaderAndRows(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "r")
	_ = os.MkdirAll(repo, 0o755)
	line := `{"time":"2026-06-01T00:00:00Z","repo":"r","accept":false,"refs":["refs/heads/main"],"findings":[{"id":"sec/x","severity":"BLOCK"}]}`
	_ = os.WriteFile(filepath.Join(repo, "audit.log"), []byte(line+"\n"), 0o600)

	var buf bytes.Buffer
	if err := ExportDecisions(root, Filter{}, ExportCSV, &buf); err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(&buf).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("want header + 1 row, got %d rows", len(rows))
	}
	if rows[0][0] != "time" || rows[0][3] != "accept" {
		t.Fatalf("unexpected header: %v", rows[0])
	}
	if rows[1][1] != "r" || rows[1][3] != "false" || rows[1][5] != "1" {
		t.Fatalf("unexpected data row: %v", rows[1])
	}
}
