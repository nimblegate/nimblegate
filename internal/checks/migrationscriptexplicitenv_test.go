// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/engine"
)

// writeScript writes a shell script with the given body under scripts/<name>.sh.
func writeScript(t *testing.T, root, name, body string) {
	t.Helper()
	dir := filepath.Join(root, "scripts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationScript_BlocksTheOriginalIncident(t *testing.T) {
	// This is approximately the apply-multi-country-migration.sh shape.
	root := t.TempDir()
	writeScript(t, root, "apply-multi-country-migration.sh", `#!/usr/bin/env bash
set -euo pipefail

SCOPE="${1:-}"
DB_NAME="myapp"

wrangler d1 execute "$DB_NAME" --file=migrations/multi-country.sql $SCOPE
`)
	got := MigrationScriptExplicitEnv(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s; want BLOCK\nreason: %s", got.Outcome, got.Reason)
	}
	if !strings.Contains(got.Reason, "wrangler") {
		t.Errorf("expected reason to flag wrangler; got: %s", got.Reason)
	}
}

func TestMigrationScript_PassesWithExplicitFlag(t *testing.T) {
	root := t.TempDir()
	writeScript(t, root, "apply-fixed.sh", `#!/usr/bin/env bash
SCOPE="${1:?usage: $0 <local|remote>}"
wrangler d1 execute "$DB" --file=migration.sql --remote
`)
	got := MigrationScriptExplicitEnv(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (--remote present on the line)\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestMigrationScript_PassesWithExplicitProjectFlag(t *testing.T) {
	root := t.TempDir()
	writeScript(t, root, "deploy.sh", `#!/usr/bin/env bash
gcloud sql databases describe mydb --project="$PROJECT"
`)
	got := MigrationScriptExplicitEnv(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (--project present)\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestMigrationScript_FlagsKubectlWithoutContext(t *testing.T) {
	root := t.TempDir()
	writeScript(t, root, "apply-noindex-migration.sh", `#!/usr/bin/env bash
kubectl apply -f manifests/noindex.yaml
`)
	got := MigrationScriptExplicitEnv(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("outcome = %s; want BLOCK (kubectl with no --context)\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestMigrationScript_NonMultiEnvCLIIgnored(t *testing.T) {
	root := t.TempDir()
	writeScript(t, root, "build.sh", `#!/usr/bin/env bash
SCOPE="${1:-}"
go build ./...
npm install
`)
	got := MigrationScriptExplicitEnv(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (no multi-env CLI invocation)", got.Outcome)
	}
}

func TestMigrationScript_RespectsLineDisable(t *testing.T) {
	root := t.TempDir()
	writeScript(t, root, "dev.sh", `#!/usr/bin/env bash
SCOPE="${1:-}"
# appframes:disable-next-line database/migration-script-explicit-env
wrangler d1 execute "$DB" --file=fixture.sql
`)
	got := MigrationScriptExplicitEnv(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (line disabled)\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestMigrationScript_FileLevelDisable(t *testing.T) {
	root := t.TempDir()
	writeScript(t, root, "dev-only.sh", `#!/usr/bin/env bash
# appframes:disable database/migration-script-explicit-env
SCOPE="${1:-}"
wrangler d1 execute "$DB" --file=fixture.sql
`)
	got := MigrationScriptExplicitEnv(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (file-level disable)\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestMigrationScript_PassesWranglerPagesDeploy(t *testing.T) {
	// `wrangler pages deploy` is always remote - no local/remote footgun.
	root := t.TempDir()
	writeScript(t, root, "deploy-studio.sh", `#!/usr/bin/env bash
set -euo pipefail
wrangler pages deploy .svelte-kit/cloudflare \
  --project-name=studio-myapp \
  --branch=main
`)
	got := MigrationScriptExplicitEnv(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (pages deploy has no local/remote footgun)\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestMigrationScript_PassesWorkerDeploy(t *testing.T) {
	// `wrangler deploy` (Workers) is always remote.
	root := t.TempDir()
	writeScript(t, root, "deploy-workers.sh", `#!/usr/bin/env bash
set -euo pipefail
wrangler deploy
`)
	got := MigrationScriptExplicitEnv(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (worker deploy has no local/remote footgun)\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestMigrationScript_PassesValidatedScopeVar(t *testing.T) {
	// The myapp apply-*-migration.sh pattern: SCOPE is defaulted but
	// VALIDATED against --local/--remote before use, then passed to wrangler.
	// That is an explicit env flag (indirect) - must NOT fire.
	root := t.TempDir()
	writeScript(t, root, "apply-noindex-migration.sh", `#!/usr/bin/env bash
set -euo pipefail
DB_NAME="demo-studio"
SCOPE="${1:-}"
if [[ "$SCOPE" != "--local" && "$SCOPE" != "--remote" ]]; then
  echo "Usage: $0 --remote | --local" >&2
  exit 1
fi
wrangler d1 execute "$DB_NAME" $SCOPE --json --command "SELECT 1"
`)
	got := MigrationScriptExplicitEnv(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (SCOPE validated against --local/--remote)\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestMigrationScript_BlocksDefaultedUnvalidatedScope(t *testing.T) {
	// Regression guard for the Fix-B suppression: a DEFAULTED scope var that
	// is NOT validated (the original incident) must still BLOCK.
	root := t.TempDir()
	writeScript(t, root, "apply-bad-migration.sh", `#!/usr/bin/env bash
DB_NAME="demo-studio"
SCOPE="${1:-}"
wrangler d1 execute "$DB_NAME" $SCOPE --command "SELECT 1"
`)
	got := MigrationScriptExplicitEnv(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("outcome = %s; want BLOCK (defaulted SCOPE with no validation guard)\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestMigrationScript_ApplyPrefixedExtensionless(t *testing.T) {
	// `apply-foo-migration` (no extension) should still be scanned.
	root := t.TempDir()
	dir := filepath.Join(root, "scripts")
	_ = os.MkdirAll(dir, 0o755)
	body := `#!/usr/bin/env bash
SCOPE="${1:-}"
wrangler kv put "$KEY" "$VALUE"
`
	_ = os.WriteFile(filepath.Join(dir, "apply-flags-migration"), []byte(body), 0o755)
	got := MigrationScriptExplicitEnv(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("outcome = %s; want BLOCK (apply-*-migration shape should be scanned)\nreason: %s", got.Outcome, got.Reason)
	}
}
