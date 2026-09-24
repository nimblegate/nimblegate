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
	got := resolveLocalImport(root, page, "./SitePanel.svelte")
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
	got := resolveLocalImport(root, page, "$lib/Widget.svelte")
	if got != target {
		t.Errorf("got %q; want %q", got, target)
	}
}

func TestResolveLocalImport_External(t *testing.T) {
	got := resolveLocalImport("/some", "/some/page.svelte", "external-package")
	if got != "" {
		t.Errorf("external imports should return empty; got %q", got)
	}
}

// An import is pushed text: a relative path or a $lib lookup that climbs out of
// the scanned tree must never resolve, even when the target file exists.
func TestResolveLocalImport_NeverLeavesRoot(t *testing.T) {
	outside := t.TempDir()
	root := filepath.Join(outside, "repo")
	page := filepath.Join(root, "src/routes/+page.svelte")
	_ = os.MkdirAll(filepath.Dir(page), 0o755)
	secret := filepath.Join(outside, "secret.svelte")
	_ = os.WriteFile(secret, []byte("import { env } from '$env/dynamic/public'"), 0o644)
	_ = os.MkdirAll(filepath.Join(outside, "src", "lib"), 0o755)
	_ = os.WriteFile(filepath.Join(outside, "src", "lib", "Up.svelte"), []byte(""), 0o644)

	for _, imp := range []string{"../../../secret.svelte", "../../../secret", "$lib/Up.svelte", "$lib/../../../secret.svelte"} {
		if got := resolveLocalImport(root, page, imp); got != "" {
			t.Errorf("import %q escaped the scanned tree to %q", imp, got)
		}
	}
}

func TestResolveLocalImport_RelativePageInsideRoot(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(filepath.Join(root, "src/routes"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "src/routes/SitePanel.svelte"), []byte(""), 0o644)
	t.Chdir(root)
	if got := resolveLocalImport(root, "src/routes/+page.svelte", "./SitePanel.svelte"); got == "" {
		t.Error("a relative page path under the root must still resolve its imports")
	}
}
