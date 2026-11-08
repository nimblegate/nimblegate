// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/BurntSushi/toml"

	"nimblegate/internal/engine"
)

// enabledListChecker blocks on secret.txt when the overlaid config enables
// "x/flag", and records every tree it was handed.
type enabledListChecker struct {
	calls *int
	trees *[]string
}

func (c enabledListChecker) Check(root string) ([]engine.CheckResult, []engine.SuppressionLog, error) {
	*c.calls++
	*c.trees = append(*c.trees, root)
	var cfg struct {
		Frames struct {
			Enabled []string `toml:"enabled"`
		} `toml:"frames"`
		Scan struct {
			Exclude []string `toml:"exclude"`
		} `toml:"scan"`
	}
	if _, err := toml.DecodeFile(filepath.Join(root, "appframes.toml"), &cfg); err != nil {
		return nil, nil, err
	}
	on := false
	for _, id := range cfg.Frames.Enabled {
		on = on || id == "x/flag"
	}
	var out []engine.CheckResult
	if _, err := os.Stat(filepath.Join(root, "secret.txt")); err == nil && on {
		out = append(out, engine.CheckResult{FrameID: "x/flag", Outcome: engine.OutcomeBlock, Reason: "secret",
			Hits: []engine.Hit{{File: filepath.Join(root, "secret.txt"), Line: 1, Label: "secret shape"}}})
	}
	if len(cfg.Scan.Exclude) != 1 {
		out = append(out, engine.CheckResult{FrameID: "x/scan-config-lost", Outcome: engine.OutcomeWarn, Reason: "lost"})
	}
	return out, nil, nil
}

func previewFixture(t *testing.T, files map[string]string) (PreviewInput, *int, *[]string) {
	t.Helper()
	policyDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(policyDir, "appframes.toml"), []byte("[frames]\nenabled = [\"x/other\"]\n\n[scan]\nexclude = [\"vendor\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	calls, trees := 0, []string{}
	return PreviewInput{
		Policy:     Policy{Repo: "r", Enabled: true, ProtectedRefs: []string{"refs/heads/*"}, PolicyDir: policyDir},
		GitDir:     makeBareWithFiles(t, files),
		Checker:    enabledListChecker{calls: &calls, trees: &trees},
		ScanTmpDir: t.TempDir(),
	}, &calls, &trees
}

func TestPreviewPolicy_candidateListDecidesAndStoredPolicyUntouched(t *testing.T) {
	in, _, trees := previewFixture(t, map[string]string{"secret.txt": "k\n"})
	before, _ := os.ReadFile(filepath.Join(in.Policy.PolicyDir, "appframes.toml"))

	in.Enabled = []string{"x/flag"}
	res := PreviewPolicy(in)
	if res.Ref != "refs/heads/main" || res.Rev == "" {
		t.Fatalf("ref = %q rev = %q", res.Ref, res.Rev)
	}
	if res.BlockCount != 1 || len(res.Blocks) != 1 || res.Blocks[0].File != "secret.txt" || res.Blocks[0].Line != 1 || res.Blocks[0].Frame != "x/flag" {
		t.Fatalf("blocks = %+v (count %d)", res.Blocks, res.BlockCount)
	}
	if res.WarnCount != 0 {
		t.Errorf("the overlay dropped [scan] config: %+v", res.Warns)
	}
	if !res.WouldReject() {
		t.Error("a block must mean the push would be rejected")
	}

	in.Enabled = []string{"x/other"}
	if res := PreviewPolicy(in); res.BlockCount != 0 {
		t.Errorf("without the frame: blocks = %+v", res.Blocks)
	}

	after, _ := os.ReadFile(filepath.Join(in.Policy.PolicyDir, "appframes.toml"))
	if string(before) != string(after) {
		t.Errorf("stored policy changed:\n%s", after)
	}
	for _, d := range *trees {
		if _, err := os.Stat(d); !os.IsNotExist(err) {
			t.Errorf("preview tree %s not removed", d)
		}
	}
}

func TestPreviewPolicy_cacheHitSkipsRescan(t *testing.T) {
	in, calls, _ := previewFixture(t, map[string]string{"secret.txt": "k\n"})
	in.Enabled = []string{"x/flag"}
	a := PreviewPolicy(in)
	b := PreviewPolicy(in)
	if *calls != 1 {
		t.Fatalf("checker ran %d times for identical inputs, want 1", *calls)
	}
	if a.BlockCount != b.BlockCount || a.Rev != b.Rev {
		t.Errorf("cached result differs: %+v vs %+v", a, b)
	}
	// A whitelist edit changes the key.
	wl := filepath.Join(in.Policy.PolicyDir, ".appframes", "_canonical", "whitelist.toml")
	os.MkdirAll(filepath.Dir(wl), 0o755)
	os.WriteFile(wl, []byte("# changed\n"), 0o644)
	PreviewPolicy(in)
	if *calls != 2 {
		t.Errorf("whitelist change must invalidate the cache; checker ran %d times", *calls)
	}
}

func TestPreviewPolicy_noCodeDisabledAndUngated(t *testing.T) {
	empty := filepath.Join(t.TempDir(), "e.git")
	if out, err := exec.Command("git", "init", "-q", "--bare", empty).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	in, calls, _ := previewFixture(t, map[string]string{"a": "b"})
	gitDir := in.GitDir

	in.GitDir = empty
	if res := PreviewPolicy(in); !res.NoCode {
		t.Errorf("empty repo: %+v", res)
	}
	in.GitDir = gitDir
	in.Policy.ProtectedRefs = []string{"refs/heads/release"}
	if res := PreviewPolicy(in); !res.NoGatedRef {
		t.Errorf("no gated ref: %+v", res)
	}
	in.Policy.Enabled = false
	if res := PreviewPolicy(in); !res.Disabled {
		t.Errorf("disabled repo: %+v", res)
	}
	if *calls != 0 {
		t.Errorf("nothing to scan, yet the checker ran %d times", *calls)
	}
}

func TestPreviewPolicy_scanFailureReported(t *testing.T) {
	in, _, _ := previewFixture(t, map[string]string{"big.txt": "0123456789"})
	in.MaxTreeBytes = 1
	res := PreviewPolicy(in)
	if res.ScanError == "" || !res.WouldReject() {
		t.Fatalf("an oversized tree must surface as a scan failure: %+v", res)
	}
}
