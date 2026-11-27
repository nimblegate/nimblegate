// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/engine"
	"nimblegate/internal/stdlib"
)

// liveStripe builds a real-shaped key at run time so no literal key is in
// the file.
func liveStripe(seed int64) string {
	const an = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
	r := rand.New(rand.NewSource(seed))
	b := make([]byte, 28)
	for i := range b {
		b[i] = an[r.Intn(len(an))]
	}
	return "sk_" + "live_" + string(b)
}

// keyRun checks root with the two key frames plus one style frame and
// returns frame -> reason for every non-pass result.
func keyRun(t *testing.T, root string, local bool) map[string]string {
	t.Helper()
	all, err := stdlib.Load()
	if err != nil {
		t.Fatal(err)
	}
	e, err := engine.New(engine.Options{ProjectRoot: root, StdlibFrames: all, CheckFuncs: BuiltinCheckFuncs()})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	ctx := engine.CheckContext{
		Trigger:       engine.TriggerCLI,
		ProjectRoot:   root,
		WorkingDir:    root,
		ExcludedDirs:  e.ExcludedDirs(),
		IgnorePath:    e.IgnorePathFunc(),
		IgnoreKeyPath: e.IgnoreKeyPathFunc(local),
	}
	out := map[string]string{}
	for _, r := range engine.Run(e.Registry, ctx) {
		if r.Outcome != engine.OutcomePass {
			out[r.FrameID] = string(r.Outcome) + ": " + r.Reason
		}
	}
	return out
}

func writeKeyProject(t *testing.T, root, scan string) {
	t.Helper()
	cfg := "[project]\nname = \"t\"\n\n[frames]\nenabled = [\"security/no-hardcoded-credentials\", \"security/no-private-keys-in-repo\", \"security/no-zero-width-in-source\"]\n" + scan
	files := map[string]string{
		"appframes.toml":     cfg,
		"dist/assets/app.js": "var a=1;const s=\"" + liveStripe(1) + "\";var b=\"x — y\";\n",
		"build/out.js":       "const t=\"" + liveStripe(2) + "\";\n",
		"node_modules/m.js":  "const n=\"" + liveStripe(3) + "\";\n",
		"src/zw.go":          "package src\n\nvar x\u200b = 1\n",
		"dist/zw.go":         "package src\n\nvar x\u200b = 1\n",
	}
	for p, c := range files {
		full := filepath.Join(root, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

const credFrame = "security/no-hardcoded-credentials"

// TestKeyFrames_ReadBuildOutputByDefault: under the default excludes a key in
// dist/ or build/ blocks (gateway and outside git alike), node_modules stays
// skipped, and other frames still skip build output.
func TestKeyFrames_ReadBuildOutputByDefault(t *testing.T) {
	for _, local := range []bool{false, true} {
		root := t.TempDir()
		writeKeyProject(t, root, "")
		got := keyRun(t, root, local)
		r := got[credFrame]
		if !strings.HasPrefix(r, "BLOCK") || !strings.Contains(r, "dist/assets/app.js:1") || !strings.Contains(r, "build/out.js:1") {
			t.Fatalf("local=%v: want BLOCK on dist and build, got %q", local, r)
		}
		if strings.Contains(r, "node_modules") {
			t.Errorf("local=%v: node_modules should stay skipped: %q", local, r)
		}
		if em := got["security/no-zero-width-in-source"]; !strings.Contains(em, "src/zw.go") || strings.Contains(em, "dist/") {
			t.Errorf("local=%v: other frames should flag src/ and skip dist/: %q", local, em)
		}
	}
}

// TestKeyFrames_ExplicitExcludeWins: a [scan] exclude naming dist and build
// is honoured by the key frames too.
func TestKeyFrames_ExplicitExcludeWins(t *testing.T) {
	root := t.TempDir()
	writeKeyProject(t, root, "\n[scan]\nexclude = [\"node_modules\", \"dist\", \"build\"]\n")
	if r, ok := keyRun(t, root, false)[credFrame]; ok {
		t.Fatalf("explicit exclude ignored: %q", r)
	}
}

// TestKeyFrames_LocalCheckFollowsGitignore: in a working folder, a gitignored
// dist/ is not read (it never leaves the machine), a committed build/ is.
// The gateway (local=false) reads both.
func TestKeyFrames_LocalCheckFollowsGitignore(t *testing.T) {
	root := t.TempDir()
	writeKeyProject(t, root, "")
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("dist/\nnode_modules/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"-c", "user.email=t@example.com", "-c", "user.name=t", "add", "-A"},
		{"-c", "user.email=t@example.com", "-c", "user.name=t", "commit", "-q", "--no-verify", "-m", "x"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	// An untracked, not ignored file under build/ would be committed by the
	// next `git add -A`, so it is read too.
	if err := os.WriteFile(filepath.Join(root, "build", "new.js"), []byte("const u=\""+liveStripe(4)+"\";\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := keyRun(t, root, true)[credFrame]
	if strings.Contains(r, "dist/") {
		t.Errorf("local check read gitignored dist/: %q", r)
	}
	if !strings.Contains(r, "build/out.js:1") || !strings.Contains(r, "build/new.js:1") {
		t.Errorf("local check missed committable build/ files: %q", r)
	}
	r = keyRun(t, root, false)[credFrame]
	if !strings.Contains(r, "dist/assets/app.js:1") {
		t.Errorf("gateway mode skipped dist/: %q", r)
	}
}
