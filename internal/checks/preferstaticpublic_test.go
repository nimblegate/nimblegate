// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"strings"
	"testing"

	"nimblegate/internal/engine"
)

func TestPreferStaticPublic_InfoOnDynamicImport(t *testing.T) {
	root := t.TempDir()
	writeSvelte(t, root, "src/Panel.svelte", `<script>
  import { env } from '$env/dynamic/public';
  const x = env.PUBLIC_API_URL;
</script>
`)
	got := PreferStaticPublic(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeInfo {
		t.Errorf("outcome = %s; want INFO\nreason: %s", got.Outcome, got.Reason)
	}
	if !strings.Contains(got.Reason, "static/public") {
		t.Errorf("expected reason to suggest static/public; got: %s", got.Reason)
	}
}

func TestPreferStaticPublic_PassesOnStaticImport(t *testing.T) {
	root := t.TempDir()
	writeSvelte(t, root, "src/Panel.svelte", `<script>
  import { PUBLIC_API_URL } from '$env/static/public';
  const x = PUBLIC_API_URL;
</script>
`)
	got := PreferStaticPublic(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (static is fine)\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestPreferStaticPublic_PassesOnNoImport(t *testing.T) {
	root := t.TempDir()
	writeSvelte(t, root, "src/Plain.svelte", `<script>
  let count = 0;
</script>
<button on:click={() => count++}>{count}</button>
`)
	got := PreferStaticPublic(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (no env import)\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestPreferStaticPublic_LineDisableSuppresses(t *testing.T) {
	root := t.TempDir()
	writeSvelte(t, root, "src/Panel.svelte", `<script>
  // appframes:disable-next-line app-correctness/prefer-static-public
  import { env } from '$env/dynamic/public';
</script>
`)
	got := PreferStaticPublic(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS (line disabled)\nreason: %s", got.Outcome, got.Reason)
	}
}
