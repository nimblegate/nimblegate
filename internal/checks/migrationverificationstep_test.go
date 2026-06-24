// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/engine"
)

func writeMigrationWrapper(t *testing.T, root, name, body string) {
	t.Helper()
	dir := filepath.Join(root, "scripts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationVerification_BlocksApplyWithoutVerification(t *testing.T) {
	root := t.TempDir()
	writeMigrationWrapper(t, root, "apply-multi-country-migration.sh", `#!/usr/bin/env bash
set -euo pipefail
SCOPE="${1:?usage}"
DB="myapp"
wrangler d1 execute "$DB" --file=migrations/multi-country.sql --remote
echo "done"
`)
	got := MigrationVerificationStep(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s; want BLOCK\nreason: %s", got.Outcome, got.Reason)
	}
	if !strings.Contains(got.Reason, "no post-apply verification") {
		t.Errorf("expected verification-missing label; got: %s", got.Reason)
	}
}

func TestMigrationVerification_PassesWithPragmaCheck(t *testing.T) {
	root := t.TempDir()
	writeMigrationWrapper(t, root, "apply-multi-country-migration.sh", `#!/usr/bin/env bash
set -euo pipefail
SCOPE="${1:?usage}"
DB="myapp"
wrangler d1 execute "$DB" --file=migrations/multi-country.sql --remote
# Verify
wrangler d1 execute "$DB" --remote --command "SELECT name FROM pragma_table_info('posts');"
`)
	got := MigrationVerificationStep(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (pragma_table_info present)\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestMigrationVerification_PassesWithDescribe(t *testing.T) {
	root := t.TempDir()
	writeMigrationWrapper(t, root, "apply-postgres-migration.sh", `#!/usr/bin/env bash
psql "$DATABASE_URL" -f migrations/multi-country.sql
psql "$DATABASE_URL" -c "\d+ posts" | grep -q country
`)
	got := MigrationVerificationStep(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (\\d+ describe present)\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestMigrationVerification_PassesWithShowTables(t *testing.T) {
	root := t.TempDir()
	writeMigrationWrapper(t, root, "apply-mysql-migration.sh", `#!/usr/bin/env bash
mysql -u root -p"$PASS" < migrations/init.sql
mysql -u root -p"$PASS" -e "SHOW TABLES;"
`)
	got := MigrationVerificationStep(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (SHOW TABLES present)\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestMigrationVerification_IgnoresNonWrappers(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "scripts")
	_ = os.MkdirAll(dir, 0o755)
	// Not an apply-*-migration* shape - should be ignored.
	_ = os.WriteFile(filepath.Join(dir, "deploy.sh"), []byte(`#!/bin/bash
wrangler d1 execute "$DB" --file=foo.sql --remote
`), 0o644)
	got := MigrationVerificationStep(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (file is not apply-*-migration*)", got.Outcome)
	}
}

func TestMigrationVerification_IgnoresWrapperWithoutDDLApply(t *testing.T) {
	root := t.TempDir()
	writeMigrationWrapper(t, root, "apply-fake-migration.sh", `#!/bin/bash
echo "I do nothing"
date
`)
	got := MigrationVerificationStep(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (no DDL apply detected)", got.Outcome)
	}
}

func TestMigrationVerification_FileDisableSuppresses(t *testing.T) {
	root := t.TempDir()
	writeMigrationWrapper(t, root, "apply-local-migration.sh", `#!/usr/bin/env bash
# appframes:disable database/migration-verification-step
# (local emulator only)
wrangler d1 execute "$DB" --file=foo.sql --local
`)
	got := MigrationVerificationStep(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (file-disabled)", got.Outcome)
	}
}
