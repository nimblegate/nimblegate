// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/engine"
)

func writeRel(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestExtractSchemaColumns(t *testing.T) {
	sql := `
CREATE TABLE posts (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  country TEXT,
  PRIMARY KEY (id)
);

CREATE TABLE IF NOT EXISTS pages (
  id TEXT,
  slug TEXT
);

ALTER TABLE posts ADD COLUMN alternates TEXT;
ALTER TABLE posts ADD COLUMN noindex INTEGER;
`
	got := extractSchemaColumns(sql)
	want := map[string]bool{"id": true, "title": true, "country": true, "slug": true, "alternates": true, "noindex": true}
	for _, g := range got {
		if !want[g] {
			t.Errorf("unexpected col %q", g)
		}
		delete(want, g)
	}
	if len(want) != 0 {
		t.Errorf("missing cols: %v", want)
	}
}

func TestSchemaVsCodeDrift_BlocksUndeclaredColumn(t *testing.T) {
	root := t.TempDir()
	writeRel(t, root, "schema.sql", `CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT);`)
	writeRel(t, root, "db/content.js", `const POST_LIST_COLS = ['id', 'title', 'country', 'alternates'];
export { POST_LIST_COLS };
`)
	got := SchemaVsCodeDrift(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s; want BLOCK\nreason: %s", got.Outcome, got.Reason)
	}
	if !strings.Contains(got.Reason, "country") || !strings.Contains(got.Reason, "alternates") {
		t.Errorf("expected both undeclared cols in reason; got: %s", got.Reason)
	}
	if strings.Contains(got.Reason, "id") || strings.Contains(got.Reason, "title") {
		t.Errorf("declared cols should not be flagged; got: %s", got.Reason)
	}
}

func TestSchemaVsCodeDrift_PassesWhenColumnsMatch(t *testing.T) {
	root := t.TempDir()
	writeRel(t, root, "schema.sql", `CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT, country TEXT);`)
	writeRel(t, root, "db/content.js", `const POST_LIST_COLS = ['id', 'title', 'country'];`)
	got := SchemaVsCodeDrift(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestSchemaVsCodeDrift_AlterTableAddsCount(t *testing.T) {
	root := t.TempDir()
	writeRel(t, root, "schema.sql", `CREATE TABLE posts (id TEXT, title TEXT);`)
	writeRel(t, root, "migrations/0002-add-country.sql", `ALTER TABLE posts ADD COLUMN country TEXT;`)
	writeRel(t, root, "db/content.js", `const POST_LIST_COLS = ['id', 'title', 'country'];`)
	got := SchemaVsCodeDrift(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (country comes from a migration)\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestSchemaVsCodeDrift_NoSchemaPassesGracefully(t *testing.T) {
	root := t.TempDir()
	// No schema.sql, no migrations/ - can't decide, must PASS.
	writeRel(t, root, "db/content.js", `const POST_LIST_COLS = ['id', 'unknown'];`)
	got := SchemaVsCodeDrift(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (no schema = no opinion)\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestSchemaVsCodeDrift_OnlyScansDBDirs(t *testing.T) {
	root := t.TempDir()
	writeRel(t, root, "schema.sql", `CREATE TABLE posts (id TEXT);`)
	// UPPER_SNAKE_COLS const in business logic, NOT under db/. Should be
	// ignored.
	writeRel(t, root, "lib/business.js", `const SOMETHING_COLS = ['x', 'y', 'unknown'];`)
	got := SchemaVsCodeDrift(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (consts outside db/ ignored)", got.Outcome)
	}
}

func TestSchemaVsCodeDrift_LineDisableSuppresses(t *testing.T) {
	root := t.TempDir()
	writeRel(t, root, "schema.sql", `CREATE TABLE posts (id TEXT);`)
	writeRel(t, root, "db/content.js", `// appframes:disable-next-line database/schema-vs-code-drift
const POST_VIRTUAL_COLS = ['id', 'computed_url'];
const POST_LIST_COLS = ['id', 'unknown'];
`)
	got := SchemaVsCodeDrift(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s; want BLOCK (the second const should fire)\nreason: %s", got.Outcome, got.Reason)
	}
	if strings.Contains(got.Reason, "computed_url") {
		t.Errorf("computed_url was on a line-disabled const; should not be flagged; got: %s", got.Reason)
	}
	if !strings.Contains(got.Reason, "unknown") {
		t.Errorf("expected 'unknown' in reason; got: %s", got.Reason)
	}
}

func TestSchemaVsCodeDrift_AcceptsBackticksAndSingleQuotes(t *testing.T) {
	root := t.TempDir()
	writeRel(t, root, "schema.sql", `CREATE TABLE posts (id TEXT, slug TEXT);`)
	writeRel(t, root, "db/content.js", "const POST_LIST_COLS = [`id`, `slug`];\n")
	got := SchemaVsCodeDrift(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (backtick literals should be supported)\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestSchemaVsCodeDrift_ExportConst(t *testing.T) {
	root := t.TempDir()
	writeRel(t, root, "schema.sql", `CREATE TABLE posts (id TEXT);`)
	writeRel(t, root, "db/content.ts", `export const POST_LIST_COLS: string[] = ['id', 'missing'];`)
	got := SchemaVsCodeDrift(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("outcome = %s; want BLOCK\nreason: %s", got.Outcome, got.Reason)
	}
}
