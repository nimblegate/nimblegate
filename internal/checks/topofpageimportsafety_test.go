// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/engine"
)

func TestTopOfPageImportSafety_FlagsRootImportOfDynamicEnvComponent(t *testing.T) {
	root := t.TempDir()
	writeRel(t, root, "src/routes/+page.svelte", `<script lang="ts">
  import SitePanel from './SitePanel.svelte';
</script>
<SitePanel />
`)
	writeRel(t, root, "src/routes/SitePanel.svelte", `<script lang="ts">
  import { env } from '$env/dynamic/public';
  const flag = env.PUBLIC_FLAG;
</script>
`)
	got := TopOfPageImportSafety(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeInfo {
		t.Fatalf("outcome = %s; want INFO\nreason: %s", got.Outcome, got.Reason)
	}
	if !strings.Contains(got.Reason, "SitePanel") {
		t.Errorf("expected SitePanel in reason; got: %s", got.Reason)
	}
}

func TestTopOfPageImportSafety_PassesWhenComponentAvoidsDynamicEnv(t *testing.T) {
	root := t.TempDir()
	writeRel(t, root, "src/routes/+page.svelte", `<script>
  import Safe from './Safe.svelte';
</script>
`)
	writeRel(t, root, "src/routes/Safe.svelte", `<script>
  let count = 0;
</script>
<button on:click={() => count++}>{count}</button>
`)
	got := TopOfPageImportSafety(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestTopOfPageImportSafety_IgnoresExternalImports(t *testing.T) {
	root := t.TempDir()
	writeRel(t, root, "src/routes/+page.svelte", `<script>
  import { Button } from 'some-ui-lib';
</script>
`)
	got := TopOfPageImportSafety(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (external imports out of scope)", got.Outcome)
	}
}

func TestTopOfPageImportSafety_FlagsLayoutSvelte(t *testing.T) {
	root := t.TempDir()
	writeRel(t, root, "src/routes/+layout.svelte", `<script>
  import Header from './Header.svelte';
</script>
<Header />
<slot />
`)
	writeRel(t, root, "src/routes/Header.svelte", `<script>
  import { env } from '$env/dynamic/public';
</script>
`)
	got := TopOfPageImportSafety(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeInfo {
		t.Errorf("outcome = %s; want INFO (+layout.svelte should also be scanned)\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestTopOfPageImportSafety_LineDisableSuppresses(t *testing.T) {
	root := t.TempDir()
	writeRel(t, root, "src/routes/+page.svelte", `<script>
  <!-- appframes:disable-next-line app-correctness/top-of-page-import-safety -->
  import SitePanel from './SitePanel.svelte';
</script>
`)
	writeRel(t, root, "src/routes/SitePanel.svelte", `<script>
  import { env } from '$env/dynamic/public';
</script>
`)
	got := TopOfPageImportSafety(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (line disabled)\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestTopOfPageImportSafety_FileDisableSuppresses(t *testing.T) {
	root := t.TempDir()
	writeRel(t, root, "src/routes/+page.svelte", `<!-- appframes:disable app-correctness/top-of-page-import-safety -->
<script>
  import SitePanel from './SitePanel.svelte';
</script>
`)
	writeRel(t, root, "src/routes/SitePanel.svelte", `<script>
  import { env } from '$env/dynamic/public';
</script>
`)
	got := TopOfPageImportSafety(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (file disable)\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestResolveLocalImport_RelativePath(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "src/routes/+page.svelte")
	target := filepath.Join(root, "src/routes/SitePanel.svelte")
	_ = os.MkdirAll(filepath.Dir(page), 0o755)
	_ = os.WriteFile(target, []byte(""), 0o644)
	got := resolveLocalImport(page, "./SitePanel.svelte")
	if got != target {
		t.Errorf("got %q; want %q", got, target)
	}
}

func TestResolveLocalImport_LibAlias(t *testing.T) {
	root := t.TempDir()
	page := filepath.Join(root, "src/routes/+page.svelte")
	target := filepath.Join(root, "src/lib/Widget.svelte")
	_ = os.MkdirAll(filepath.Dir(page), 0o755)
	_ = os.MkdirAll(filepath.Dir(target), 0o755)
	_ = os.WriteFile(target, []byte(""), 0o644)
	got := resolveLocalImport(page, "$lib/Widget.svelte")
	if got != target {
		t.Errorf("got %q; want %q", got, target)
	}
}

func TestResolveLocalImport_External(t *testing.T) {
	got := resolveLocalImport("/some/page.svelte", "external-package")
	if got != "" {
		t.Errorf("external imports should return empty; got %q", got)
	}
}
