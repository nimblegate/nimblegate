// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/engine"
)

func writeSQL(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSQLiteMigration_BlocksUnwrappedAddColumn(t *testing.T) {
	root := t.TempDir()
	writeSQL(t, root, "migrations/multi-country.sql", `ALTER TABLE posts ADD COLUMN country TEXT;
ALTER TABLE pages ADD COLUMN country TEXT;
CREATE INDEX idx_posts_country ON posts(country);
`)
	got := SQLiteMigrationIdempotentWrapper(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s; want BLOCK\nreason: %s", got.Outcome, got.Reason)
	}
	if !strings.Contains(got.Reason, "ADD COLUMN") {
		t.Errorf("expected reason to mention ADD COLUMN; got: %s", got.Reason)
	}
}

func TestSQLiteMigration_PassesWithWrapperScript(t *testing.T) {
	root := t.TempDir()
	writeSQL(t, root, "migrations/multi-country.sql", `ALTER TABLE posts ADD COLUMN country TEXT;`)
	// Wrapper present → BLOCK is suppressed (responsibility shifts to the
	// migration-script-explicit-env frame on the wrapper itself).
	dir := filepath.Join(root, "scripts")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "apply-multi-country-migration.sh"), []byte("#!/usr/bin/env bash\n"), 0o755)

	got := SQLiteMigrationIdempotentWrapper(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (wrapper present)\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestSQLiteMigration_PassesWithOptOutComment(t *testing.T) {
	root := t.TempDir()
	writeSQL(t, root, "migrations/historical-2026-01.sql", `-- IDEMPOTENT-WRAPPER-NOT-REQUIRED
-- One-time table create for the 2026-01 launch; never re-applied.
ALTER TABLE posts ADD COLUMN legacy_country TEXT;
`)
	got := SQLiteMigrationIdempotentWrapper(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (opt-out comment)\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestSQLiteMigration_BlocksDropColumn(t *testing.T) {
	root := t.TempDir()
	writeSQL(t, root, "migrations/cleanup.sql", `ALTER TABLE posts DROP COLUMN legacy_country;`)
	got := SQLiteMigrationIdempotentWrapper(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("outcome = %s; want BLOCK (DROP COLUMN is destructive)\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestSQLiteMigration_BlocksRenameColumn(t *testing.T) {
	root := t.TempDir()
	writeSQL(t, root, "migrations/rename.sql", `ALTER TABLE posts RENAME COLUMN country TO country_code;`)
	got := SQLiteMigrationIdempotentWrapper(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("outcome = %s; want BLOCK (RENAME COLUMN is non-idempotent)\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestSQLiteMigration_PassesOnCreateTable(t *testing.T) {
	root := t.TempDir()
	// CREATE TABLE on its own (no ALTER) is fine; SQLite supports
	// CREATE TABLE IF NOT EXISTS as a separate concern.
	writeSQL(t, root, "migrations/init.sql", `CREATE TABLE IF NOT EXISTS posts (id TEXT PRIMARY KEY, title TEXT);
CREATE INDEX idx_posts_title ON posts(title);
`)
	got := SQLiteMigrationIdempotentWrapper(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (CREATE TABLE is not in scope)\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestSQLiteMigration_IgnoresSQLOutsideMigrationsDir(t *testing.T) {
	root := t.TempDir()
	// .sql file outside migrations/ - should be ignored (seed data, ad-hoc query, etc).
	writeSQL(t, root, "seed-data.sql", `ALTER TABLE posts ADD COLUMN test TEXT;`)
	got := SQLiteMigrationIdempotentWrapper(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (file not under migrations/)", got.Outcome)
	}
}

func TestSQLiteMigration_RespectsCommentedOutALTER(t *testing.T) {
	root := t.TempDir()
	// ALTER inside a SQL comment should not fire.
	writeSQL(t, root, "migrations/notes.sql", `-- We previously did: ALTER TABLE posts ADD COLUMN test TEXT;
CREATE TABLE notes (id TEXT);
`)
	got := SQLiteMigrationIdempotentWrapper(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (ALTER inside SQL comment shouldn't fire)\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestSQLiteMigration_LineDisableSuppresses(t *testing.T) {
	root := t.TempDir()
	writeSQL(t, root, "migrations/mixed.sql", `-- appframes:disable-next-line database/sqlite-migration-idempotent-wrapper
ALTER TABLE posts ADD COLUMN test TEXT;
ALTER TABLE pages ADD COLUMN other TEXT;
`)
	got := SQLiteMigrationIdempotentWrapper(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s; want BLOCK (line 4 should still fire)\nreason: %s", got.Outcome, got.Reason)
	}
	if len(got.Hits) != 1 {
		t.Errorf("got %d hits; want 1 (only line 3 should fire after the disable)", len(got.Hits))
	}
}
