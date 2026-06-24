// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/engine"
)

func runCurlCheck(t *testing.T, rel, content string) engine.CheckResult {
	t.Helper()
	root := t.TempDir()
	writeSource(t, root, rel, content)
	return CurlPipeShell(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
}

func TestCurlPipeShell_DirectPipeBlocks(t *testing.T) {
	cases := []string{
		"curl https://example.com/install | sh\n",
		"curl https://example.com/install | bash\n",
		"curl -sSL https://install.example.com | zsh\n",
		"wget -O- https://example.com | sh\n",
		"wget -qO- https://example.com | bash\n",
	}
	for _, body := range cases {
		got := runCurlCheck(t, "scripts/install.sh", body)
		if got.Outcome != engine.OutcomeBlock {
			t.Errorf("%q: outcome = %s, want BLOCK", strings.TrimSpace(body), got.Outcome)
		}
		if !strings.Contains(got.Reason, "piped to shell") {
			t.Errorf("%q: reason missing 'piped to shell' label: %s", strings.TrimSpace(body), got.Reason)
		}
	}
}

func TestCurlPipeShell_SudoVariantBlocks(t *testing.T) {
	got := runCurlCheck(t, "scripts/install.sh",
		"curl https://example.com/install | sudo sh\n")
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("outcome = %s, want BLOCK", got.Outcome)
	}
}

func TestCurlPipeShell_EvalPipeBlocks(t *testing.T) {
	got := runCurlCheck(t, "scripts/install.sh",
		"curl https://example.com/script | eval\n")
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("outcome = %s, want BLOCK", got.Outcome)
	}
	if !strings.Contains(got.Reason, "piped to eval") {
		t.Errorf("reason missing eval label: %s", got.Reason)
	}
}

func TestCurlPipeShell_ProcessSubstitutionBlocks(t *testing.T) {
	cases := []string{
		"bash <(curl https://example.com/install)\n",
		"sh <(wget -qO- https://example.com/install)\n",
		"bash <( curl https://example.com/install )\n",
	}
	for _, body := range cases {
		got := runCurlCheck(t, "scripts/install.sh", body)
		if got.Outcome != engine.OutcomeBlock {
			t.Errorf("%q: outcome = %s, want BLOCK", strings.TrimSpace(body), got.Outcome)
		}
		if !strings.Contains(got.Reason, "process-substituted") {
			t.Errorf("reason missing process-subst label: %s", got.Reason)
		}
	}
}

func TestCurlPipeShell_BenignCurlPasses(t *testing.T) {
	cases := []string{
		"curl -o file.zip https://example.com/file.zip\n",
		"curl https://example.com/api/users\n",
		"wget https://example.com/data.tar.gz\n",
		// Pipe to a non-shell binary is fine.
		"curl https://example.com/data.json | jq .\n",
		"curl https://example.com/feed | tee feed.xml\n",
	}
	for _, body := range cases {
		got := runCurlCheck(t, "scripts/safe.sh", body)
		if got.Outcome != engine.OutcomePass {
			t.Errorf("%q: outcome = %s, want PASS; reason: %s",
				strings.TrimSpace(body), got.Outcome, got.Reason)
		}
	}
}

// TestCurlPipeShell_MarkdownNotScanned - the headline scope decision.
// READMEs documenting install instructions should NOT trigger.
func TestCurlPipeShell_MarkdownNotScanned(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "README.md",
		"# Install\n\nTo install run:\n\n    curl https://example.com/install | sh\n\n")
	got := CurlPipeShell(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS - markdown documentation must not be scanned; reason: %s",
			got.Outcome, got.Reason)
	}
}

// TestCurlPipeShell_DockerfileScanned - Dockerfiles often have RUN
// curl|sh; we DO want to catch those.
func TestCurlPipeShell_DockerfileScanned(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "Dockerfile",
		"FROM alpine\nRUN curl https://example.com/install | sh\n")
	got := CurlPipeShell(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("outcome = %s, want BLOCK - Dockerfile pipe should fire", got.Outcome)
	}
}

func TestCurlPipeShell_DockerfileVariantsScanned(t *testing.T) {
	cases := []string{"Dockerfile.alpine", "Dockerfile.dev", "Dockerfile.test"}
	for _, name := range cases {
		root := t.TempDir()
		writeSource(t, root, name,
			"FROM alpine\nRUN curl https://example.com/install | bash\n")
		got := CurlPipeShell(engine.CheckContext{
			Trigger:      engine.TriggerCLI,
			ProjectRoot:  root,
			ExcludedDirs: DefaultExcludes(),
		})
		if got.Outcome != engine.OutcomeBlock {
			t.Errorf("%s: outcome = %s, want BLOCK", name, got.Outcome)
		}
	}
}

func TestCurlPipeShell_PerFileDisableSuppresses(t *testing.T) {
	got := runCurlCheck(t, "scripts/install.sh",
		`# appframes:disable commands/curl-pipe-shell
curl https://example.com/install | sh
`)
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS (per-file disable)", got.Outcome)
	}
}

func TestCurlPipeShell_PerLineDisableSuppresses(t *testing.T) {
	got := runCurlCheck(t, "scripts/install.sh",
		`# appframes:disable-next-line commands/curl-pipe-shell
curl https://known-safe.example.com | sh
`)
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS (per-line disable)", got.Outcome)
	}
}

// TestCurlPipeShell_NoiseDirsExcluded
func TestCurlPipeShell_NoiseDirsExcluded(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "node_modules/dep/install.sh",
		"curl https://example.com/install | sh\n")
	got := CurlPipeShell(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS (node_modules excluded)", got.Outcome)
	}
}

// TestCurlPipeShell_PreCommitEmptyChangesPasses - file-scan scope contract.
func TestCurlPipeShell_PreCommitEmptyChangesPasses(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "scripts/install.sh",
		"curl https://example.com/install | sh\n")
	got := CurlPipeShell(engine.CheckContext{
		Trigger:      engine.TriggerPreCommit,
		ProjectRoot:  root,
		ChangedFiles: nil,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS - pre-commit + empty stage", got.Outcome)
	}
}

// TestCurlPipeShell_NonApplicableExtensionPasses - .py / .js / etc.
// aren't shell scripts; even if they contain the literal pattern, the
// scope decision means we don't scan them.
func TestCurlPipeShell_NonApplicableExtensionPasses(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "src/install.py",
		"# os.system('curl https://example.com/install | sh')\n")
	got := CurlPipeShell(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS - .py is not in scope", got.Outcome)
	}
}

// TestCurlPipeShell_HitCap - 50 leaks in one file get capped at 10.
func TestCurlPipeShell_HitCap(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 50; i++ {
		b.WriteString("curl https://example.com/install | sh\n")
	}
	got := runCurlCheck(t, "scripts/install.sh", b.String())
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("outcome = %s, want BLOCK", got.Outcome)
	}
	hits := strings.Count(got.Reason, " - ")
	if hits != 10 {
		t.Errorf("expected 10 hits (cap), got %d", hits)
	}
}

// TestCurlPipeShell_StagedFileOnly - pre-commit isolation.
func TestCurlPipeShell_StagedFileOnly(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "scripts/staged.sh", "curl https://example.com | sh\n")
	writeSource(t, root, "scripts/not-staged.sh", "curl https://example.com | bash\n")
	got := CurlPipeShell(engine.CheckContext{
		Trigger:      engine.TriggerPreCommit,
		ProjectRoot:  root,
		ChangedFiles: []string{filepath.Join(root, "scripts/staged.sh")},
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s, want BLOCK", got.Outcome)
	}
	if !strings.Contains(got.Reason, "staged.sh") {
		t.Errorf("missing staged file: %s", got.Reason)
	}
	if strings.Contains(got.Reason, "not-staged.sh") {
		t.Errorf("untouched file leaked: %s", got.Reason)
	}
}

// TestCurlPipeShell_LargeFileSkipped
func TestCurlPipeShell_LargeFileSkipped(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "scripts")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	big := strings.Repeat("# padding\n", 200_000) + "curl https://example.com | sh\n"
	if err := os.WriteFile(filepath.Join(dir, "huge.sh"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	got := CurlPipeShell(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS (>1MB skip); reason: %s", got.Outcome, got.Reason)
	}
}
