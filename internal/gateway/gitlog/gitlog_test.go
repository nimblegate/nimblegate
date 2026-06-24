// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gitlog

import (
	"context"
	"strings"
	"testing"
)

// withRunner swaps the package runner for one returning canned output and
// recording the argv it was called with.
func withRunner(t *testing.T, out string) *[]string {
	t.Helper()
	var argv []string
	old := runner
	runner = func(ctx context.Context, name string, args ...string) (string, error) {
		argv = append([]string{name}, args...)
		return out, nil
	}
	t.Cleanup(func() { runner = old })
	return &argv
}

func TestLogParsesRecords(t *testing.T) {
	// Two records, RS=\x1e prefix, US=\x1f field sep, then --name-only files.
	out := "\x1ec16a0cdffffff\x1f2026-06-11\x1falice\x1ffeat: gateway URL\n\nbin/askimg.sh\nREADME.md\n" +
		"\x1edeadbeef000000\x1f2026-06-10\x1fjill\x1ffix: typo\n\ndocs/x.md\n"
	argv := withRunner(t, out)
	commits, err := Log(context.Background(), "/srv/repos/ai.git", Options{Since: "30 days ago", Path: "bin", Grep: "feat", Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 {
		t.Fatalf("want 2 commits, got %d: %+v", len(commits), commits)
	}
	c := commits[0]
	if c.SHA != "c16a0cdffffff" || c.Date != "2026-06-11" || c.Author != "alice" || c.Subject != "feat: gateway URL" {
		t.Errorf("record fields wrong: %+v", c)
	}
	if len(c.Files) != 2 || c.Files[0] != "bin/askimg.sh" || c.Files[1] != "README.md" {
		t.Errorf("files wrong: %+v", c.Files)
	}
	if len(commits[1].Files) != 1 || commits[1].Files[0] != "docs/x.md" {
		t.Errorf("second record files wrong: %+v", commits[1].Files)
	}
	got := strings.Join(*argv, " ")
	for _, want := range []string{"git -c safe.directory=/srv/repos/ai.git -C /srv/repos/ai.git log --branches", "--since=30 days ago", "--grep=feat", "-n 20", "-- bin"} {
		if !strings.Contains(got, want) {
			t.Errorf("argv missing %q: %s", want, got)
		}
	}
}

func TestLogEmpty(t *testing.T) {
	withRunner(t, "")
	commits, err := Log(context.Background(), "/x.git", Options{Limit: 10})
	if err != nil || len(commits) != 0 {
		t.Fatalf("empty output → no commits: %v %v", commits, err)
	}
}

func TestSafeRepoName(t *testing.T) {
	for _, ok := range []string{"ai-assistant", "appframes", "web"} {
		if got, err := SafeRepoName(ok); err != nil || got != ok {
			t.Errorf("SafeRepoName(%q) = %q, %v; want ok", ok, got, err)
		}
	}
	for _, bad := range []string{"", ".", "..", "../etc", "a/b", `a\b`, "a:b", ".hidden"} {
		if _, err := SafeRepoName(bad); err == nil {
			t.Errorf("SafeRepoName(%q) should error", bad)
		}
	}
}
