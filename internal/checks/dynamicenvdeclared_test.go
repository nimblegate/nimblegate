// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/engine"
)

func writeSvelte(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDynamicEnv_BlocksUndeclaredVar(t *testing.T) {
	root := t.TempDir()
	// No wrangler.toml, no .env.example → nothing declared.
	writeSvelte(t, root, "src/SitePanel.svelte", `<script lang="ts">
  import { env } from '$env/dynamic/public';
  const flag = env.PUBLIC_MULTI_COUNTRY_ENABLED;
</script>
`)
	got := DynamicEnvDeclared(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s; want BLOCK\nreason: %s", got.Outcome, got.Reason)
	}
	if !strings.Contains(got.Reason, "PUBLIC_MULTI_COUNTRY_ENABLED") {
		t.Errorf("expected reason to name the undeclared var; got: %s", got.Reason)
	}
}

func TestDynamicEnv_PassesWhenDeclaredInWrangler(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "wrangler.toml"), []byte(`name = "my-app"

[vars]
PUBLIC_MULTI_COUNTRY_ENABLED = "true"
PUBLIC_API_URL = "https://api.example.com"
`), 0o644)
	writeSvelte(t, root, "src/SitePanel.svelte", `<script lang="ts">
  import { env } from '$env/dynamic/public';
  const flag = env.PUBLIC_MULTI_COUNTRY_ENABLED;
</script>
`)
	got := DynamicEnvDeclared(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (wrangler.toml declares the var)\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestDynamicEnv_PassesWhenDeclaredInEnvExample(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, ".env.example"), []byte(`PUBLIC_API_URL=https://example.com
# DASHBOARD-ONLY: PUBLIC_SECRET_FLAG
`), 0o644)
	writeSvelte(t, root, "src/Panel.svelte", `<script>
  import { env } from '$env/dynamic/public';
  const a = env.PUBLIC_API_URL;
  const b = env.PUBLIC_SECRET_FLAG;
</script>
`)
	got := DynamicEnvDeclared(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (both vars declared in .env.example)\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestDynamicEnv_BlocksMixedDeclaredAndUndeclared(t *testing.T) {
	root := t.TempDir()
	_ = os.WriteFile(filepath.Join(root, "wrangler.toml"), []byte(`[vars]
PUBLIC_API_URL = "https://example.com"
`), 0o644)
	writeSvelte(t, root, "src/Panel.svelte", `<script>
  import { env } from '$env/dynamic/public';
  const a = env.PUBLIC_API_URL;       // declared
  const b = env.PUBLIC_UNDECLARED;    // not declared
</script>
`)
	got := DynamicEnvDeclared(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s; want BLOCK\nreason: %s", got.Outcome, got.Reason)
	}
	if !strings.Contains(got.Reason, "PUBLIC_UNDECLARED") {
		t.Errorf("expected PUBLIC_UNDECLARED in reason; got: %s", got.Reason)
	}
	if strings.Contains(got.Reason, "PUBLIC_API_URL") {
		t.Errorf("declared var should not be flagged; got: %s", got.Reason)
	}
}

func TestDynamicEnv_IgnoresFilesWithoutDynamicImport(t *testing.T) {
	root := t.TempDir()
	writeSvelte(t, root, "src/NoImport.svelte", `<script>
  const env = { PUBLIC_FOO: "bar" };
  console.log(env.PUBLIC_FOO);
</script>
`)
	got := DynamicEnvDeclared(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS - no $env/dynamic/public import means out of scope", got.Outcome)
	}
}

func TestDynamicEnv_LineDisableSuppresses(t *testing.T) {
	root := t.TempDir()
	writeSvelte(t, root, "src/Mixed.svelte", `<script>
  import { env } from '$env/dynamic/public';
  // appframes:disable-next-line app-correctness/dynamic-env-declared
  const flag = env.PUBLIC_EXPERIMENTAL_ONLY;
  const other = env.PUBLIC_ALSO_UNDECLARED;
</script>
`)
	got := DynamicEnvDeclared(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s; want BLOCK (the second var should still fire)\nreason: %s", got.Outcome, got.Reason)
	}
	if strings.Contains(got.Reason, "PUBLIC_EXPERIMENTAL_ONLY") {
		t.Errorf("disabled var should not be flagged; got: %s", got.Reason)
	}
	if !strings.Contains(got.Reason, "PUBLIC_ALSO_UNDECLARED") {
		t.Errorf("expected PUBLIC_ALSO_UNDECLARED in reason; got: %s", got.Reason)
	}
}

func TestExtractWranglerVars(t *testing.T) {
	content := `name = "my-app"
compatibility_date = "2026-05-18"

[vars]
PUBLIC_API_URL = "https://example.com"
PUBLIC_FEATURE_X = "on"
DATABASE_URL = "..."

[env.production]
name = "my-app-prod"
`
	got := extractWranglerVars(content)
	wantSet := map[string]bool{"PUBLIC_API_URL": true, "PUBLIC_FEATURE_X": true, "DATABASE_URL": true}
	if len(got) != 3 {
		t.Errorf("got %d vars; want 3: %v", len(got), got)
	}
	for _, g := range got {
		if !wantSet[g] {
			t.Errorf("unexpected var %q", g)
		}
	}
}

func TestExtractEnvExampleVars(t *testing.T) {
	content := `# Header comment
PUBLIC_API_URL=https://example.com
DATABASE_URL=

# DASHBOARD-ONLY: PUBLIC_SECRET_TOKEN
# DASHBOARD-ONLY: PUBLIC_FEATURE_FLAG
`
	got := extractEnvExampleVars(content)
	wantSet := map[string]bool{"PUBLIC_API_URL": true, "DATABASE_URL": true, "PUBLIC_SECRET_TOKEN": true, "PUBLIC_FEATURE_FLAG": true}
	if len(got) != 4 {
		t.Errorf("got %d vars; want 4: %v", len(got), got)
	}
	for _, g := range got {
		if !wantSet[g] {
			t.Errorf("unexpected var %q", g)
		}
	}
}
