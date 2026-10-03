// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package version

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// compose.yaml holds the only literal release version in the tree: it is what a
// reader actually runs, and a pin cannot be a link. Everything else derives from
// it or names no version at all. These two tests are what keep a release from
// shipping a stale pin, which a checklist step did not.

func TestComposePinMatchesChangelog(t *testing.T) {
	root := repoRoot(t)
	pin := composePin(t, root)

	head := regexp.MustCompile(`(?m)^## \[(\d+\.\d+\.\d+)\]`).FindStringSubmatch(readRepoFile(t, root, "CHANGELOG.md"))
	if head == nil {
		t.Fatal("CHANGELOG.md has no ## [x.y.z] release heading")
	}
	if pin != head[1] {
		t.Errorf("compose.yaml pins %s but the newest CHANGELOG entry is %s; bump the pin", pin, head[1])
	}
}

// Naming the current version anywhere else is what goes stale: past versions in
// prose are history and stay correct, so only the pin's own value is forbidden.
func TestCurrentVersionAppearsOnlyInThePin(t *testing.T) {
	root := repoRoot(t)
	pin := composePin(t, root)
	forbidden := []string{"nimblegate:" + pin, "v" + pin, "`" + pin + "`"}

	for _, rel := range []string{
		"README.md",
		"docs/PUBLISHING.md",
		".github/ISSUE_TEMPLATE/bug.yml",
	} {
		for i, line := range strings.Split(readRepoFile(t, root, rel), "\n") {
			for _, bad := range forbidden {
				if strings.Contains(line, bad) {
					t.Errorf("%s:%d names the current version %q - derive it from compose.yaml instead:\n  %s",
						rel, i+1, bad, strings.TrimSpace(line))
				}
			}
		}
	}
}

func composePin(t *testing.T, root string) string {
	t.Helper()
	m := regexp.MustCompile(`ghcr\.io/nimblegate/nimblegate:(\d+\.\d+\.\d+)`).FindStringSubmatch(readRepoFile(t, root, "compose.yaml"))
	if m == nil {
		t.Fatal("compose.yaml has no pinned ghcr.io/nimblegate/nimblegate:<x.y.z> image")
	}
	return m[1]
}

func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(filepath.Join(wd, "..", ".."))
}

func readRepoFile(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// The release workflow publishes the pinned version's CHANGELOG.md section as
// the GitHub release notes, through this same script, and refuses to release
// when it prints nothing. Running it here fails the suite before a tag does.
func TestReleaseNotesForPinnedVersion(t *testing.T) {
	awk, err := exec.LookPath("awk")
	if err != nil {
		t.Skip("awk not installed")
	}
	root := repoRoot(t)
	pin := composePin(t, root)
	out, err := exec.Command(awk, "-v", "v="+pin, "-f", filepath.Join(root, ".github", "release-notes.awk"), filepath.Join(root, "CHANGELOG.md")).Output()
	if err != nil {
		t.Fatalf("release-notes.awk: %v", err)
	}
	notes := string(out)
	if strings.TrimSpace(notes) == "" {
		t.Fatalf("CHANGELOG.md has no section for %s, so the release would fail", pin)
	}
	if strings.HasPrefix(notes, "\n") || strings.HasSuffix(notes, "\n\n") {
		t.Error("release notes start or end with a blank line")
	}
	for i, line := range strings.Split(notes, "\n") {
		if strings.HasPrefix(line, " ") {
			t.Errorf("line %d is still a wrapped continuation: %q", i+1, line)
		}
	}
}
