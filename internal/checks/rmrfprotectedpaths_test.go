// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/engine"
)

func TestRmRfProtectedPaths_RootSlashBlocks(t *testing.T) {
	got := RmRfProtectedPaths(engine.CheckContext{
		Trigger: engine.TriggerGitWrap,
		Command: "rm -rf /",
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("outcome = %s, want BLOCK", got.Outcome)
	}
	if !strings.Contains(got.Reason, "filesystem root") {
		t.Errorf("reason missing filesystem root label: %s", got.Reason)
	}
}

func TestRmRfProtectedPaths_OSConfigBlocks(t *testing.T) {
	cases := []string{
		"rm -rf /etc",
		"rm -rf /etc/nginx",
		"rm -rf /usr",
		"rm -rf /usr/local/bin",
		"rm -rf /var/log",
		"rm -rf /bin",
		"rm -rf /sbin",
		"rm -rf /lib",
		"rm -rf /lib64",
		"rm -rf /boot",
		"rm -rf /sys",
		"rm -rf /proc",
		"rm -rf /dev",
	}
	for _, cmd := range cases {
		got := RmRfProtectedPaths(engine.CheckContext{
			Trigger: engine.TriggerGitWrap,
			Command: cmd,
		})
		if got.Outcome != engine.OutcomeBlock {
			t.Errorf("%q: outcome = %s, want BLOCK", cmd, got.Outcome)
		}
	}
}

func TestRmRfProtectedPaths_HomeShortcutsBlock(t *testing.T) {
	cases := []string{
		"rm -rf $HOME",
		"rm -rf ~",
		"rm -rf ~/",
	}
	for _, cmd := range cases {
		got := RmRfProtectedPaths(engine.CheckContext{
			Trigger: engine.TriggerGitWrap,
			Command: cmd,
		})
		if got.Outcome != engine.OutcomeBlock {
			t.Errorf("%q: outcome = %s, want BLOCK", cmd, got.Outcome)
		}
		if !strings.Contains(got.Reason, "home directory") {
			t.Errorf("%q: reason missing home label: %s", cmd, got.Reason)
		}
	}
}

func TestRmRfProtectedPaths_UnexpandedVariableBlocks(t *testing.T) {
	cases := []string{
		`rm -rf "$STEAMROOT/"*`,
		`rm -rf $ROOT/`,
		`rm -rf ${PREFIX}/`,
		`rm -rf ""/`,
	}
	for _, cmd := range cases {
		got := RmRfProtectedPaths(engine.CheckContext{
			Trigger: engine.TriggerGitWrap,
			Command: cmd,
		})
		if got.Outcome != engine.OutcomeBlock {
			t.Errorf("%q: outcome = %s, want BLOCK", cmd, got.Outcome)
		}
		if !strings.Contains(got.Reason, "unexpanded") {
			t.Errorf("%q: reason missing unexpanded-variable label: %s", cmd, got.Reason)
		}
	}
}

func TestRmRfProtectedPaths_SafePathPasses(t *testing.T) {
	cases := []string{
		"rm -rf /tmp/foo",
		"rm -rf ./build",
		"rm -rf node_modules",
		"rm -rf dist/old",
		"rm -rf /home/user/project/output",
		"rm -rf scratch",
	}
	for _, cmd := range cases {
		got := RmRfProtectedPaths(engine.CheckContext{
			Trigger: engine.TriggerGitWrap,
			Command: cmd,
		})
		if got.Outcome != engine.OutcomePass {
			t.Errorf("%q: outcome = %s, want PASS; reason: %s", cmd, got.Outcome, got.Reason)
		}
	}
}

// TestRmRfProtectedPaths_NonRecursiveSkipped - a bare `rm /etc/file` is
// out of scope (one-file bounded risk; not what this frame guards).
func TestRmRfProtectedPaths_NonRecursiveSkipped(t *testing.T) {
	cases := []string{
		"rm /etc/foo",
		"rm /tmp/cache",
		"rm -f /etc/something",
		"rm --interactive /usr/file",
	}
	for _, cmd := range cases {
		got := RmRfProtectedPaths(engine.CheckContext{
			Trigger: engine.TriggerGitWrap,
			Command: cmd,
		})
		if got.Outcome != engine.OutcomeSkip {
			t.Errorf("%q: outcome = %s, want SKIP (no recursive flag)", cmd, got.Outcome)
		}
	}
}

// TestRmRfProtectedPaths_NonRmCommandSkipped - only the rm tool is in scope.
func TestRmRfProtectedPaths_NonRmCommandSkipped(t *testing.T) {
	cases := []string{
		"git push --force origin main",
		"apt purge rpcbind",
		"echo rm -rf /",
		"",
	}
	for _, cmd := range cases {
		got := RmRfProtectedPaths(engine.CheckContext{
			Trigger: engine.TriggerGitWrap,
			Command: cmd,
		})
		if got.Outcome != engine.OutcomeSkip {
			t.Errorf("%q: outcome = %s, want SKIP", cmd, got.Outcome)
		}
	}
}

// TestRmRfProtectedPaths_AllRecursiveFlagFormsTrigger - combined short
// flags, long form, separated short.
func TestRmRfProtectedPaths_AllRecursiveFlagFormsTrigger(t *testing.T) {
	cases := []string{
		"rm -r /etc",
		"rm -R /etc",
		"rm --recursive /etc",
		"rm -rf /etc",
		"rm -fr /etc",
		"rm -Rf /etc",
		"rm -fR /etc",
		"rm -rfv /etc",  // combined with verbose
		"rm -r -f /etc", // separate short flags
	}
	for _, cmd := range cases {
		got := RmRfProtectedPaths(engine.CheckContext{
			Trigger: engine.TriggerGitWrap,
			Command: cmd,
		})
		if got.Outcome != engine.OutcomeBlock {
			t.Errorf("%q: outcome = %s, want BLOCK (recursive flag form not detected?)",
				cmd, got.Outcome)
		}
	}
}

// TestRmRfProtectedPaths_QuotedPathRecognised
func TestRmRfProtectedPaths_QuotedPathRecognised(t *testing.T) {
	cases := []string{
		`rm -rf "/etc"`,
		`rm -rf '/etc/nginx'`,
	}
	for _, cmd := range cases {
		got := RmRfProtectedPaths(engine.CheckContext{
			Trigger: engine.TriggerGitWrap,
			Command: cmd,
		})
		if got.Outcome != engine.OutcomeBlock {
			t.Errorf("%q: outcome = %s, want BLOCK (quoted path stripping?)", cmd, got.Outcome)
		}
	}
}

// TestRmRfProtectedPaths_CustomCatalog - project canonical table
// extends the catalog.
func TestRmRfProtectedPaths_CustomCatalog(t *testing.T) {
	root := t.TempDir()
	canon := filepath.Join(root, ".appframes", "_canonical")
	if err := os.MkdirAll(canon, 0o755); err != nil {
		t.Fatal(err)
	}
	tomlBody := `
[paths]
"/srv/projects/critical-data" = "production data"
`
	if err := os.WriteFile(filepath.Join(canon, "protected-paths.toml"), []byte(tomlBody), 0o644); err != nil {
		t.Fatal(err)
	}
	got := RmRfProtectedPaths(engine.CheckContext{
		Trigger:     engine.TriggerGitWrap,
		ProjectRoot: root,
		Command:     "rm -rf /srv/projects/critical-data",
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("outcome = %s, want BLOCK on project-defined protected path; reason: %s",
			got.Outcome, got.Reason)
	}
	if !strings.Contains(got.Reason, "production data") {
		t.Errorf("reason should include project's own reason text: %s", got.Reason)
	}
}

// TestRmRfProtectedPaths_CustomCatalogExtendsNotReplaces - adding a
// project path doesn't disable the built-in catalog.
func TestRmRfProtectedPaths_CustomCatalogExtendsNotReplaces(t *testing.T) {
	root := t.TempDir()
	canon := filepath.Join(root, ".appframes", "_canonical")
	if err := os.MkdirAll(canon, 0o755); err != nil {
		t.Fatal(err)
	}
	tomlBody := `
[paths]
"/srv/projects/data" = "production data"
`
	if err := os.WriteFile(filepath.Join(canon, "protected-paths.toml"), []byte(tomlBody), 0o644); err != nil {
		t.Fatal(err)
	}
	// Built-in /etc still protected.
	got := RmRfProtectedPaths(engine.CheckContext{
		Trigger:     engine.TriggerGitWrap,
		ProjectRoot: root,
		Command:     "rm -rf /etc",
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("built-in /etc no longer protected after adding custom paths: %s", got.Outcome)
	}
}
