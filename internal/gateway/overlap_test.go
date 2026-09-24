// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// overlapFixture is a work clone plus a bare repo whose HEAD is main. Branches
// are made from main in the work clone and pushed to the bare repo.
type overlapFixture struct {
	t    *testing.T
	work string
	bare string
}

func newOverlapFixture(t *testing.T) *overlapFixture {
	t.Helper()
	f := &overlapFixture{t: t, work: t.TempDir(), bare: t.TempDir()}
	f.git(f.work, "", "init", "-q", "-b", "main")
	f.write(map[string]string{"a.txt": "a\n", "b.txt": "b\n", "c.txt": "c\n"})
	f.git(f.work, "", "add", ".")
	f.git(f.work, "", "commit", "-qm", "base")
	f.git(f.bare, "", "init", "--bare", "-q")
	f.git(f.bare, "", "symbolic-ref", "HEAD", "refs/heads/main")
	f.git(f.work, "", "push", "-q", f.bare, "main:refs/heads/main")
	return f
}

func (f *overlapFixture) git(dir, committerDate string, args ...string) string {
	f.t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if committerDate != "" {
		c.Env = append(c.Env, "GIT_COMMITTER_DATE="+committerDate)
	}
	out, err := c.CombinedOutput()
	if err != nil {
		f.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (f *overlapFixture) write(files map[string]string) {
	f.t.Helper()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(f.work, name), []byte(body), 0o644); err != nil {
			f.t.Fatal(err)
		}
	}
}

// branch commits edits to the named files on a new branch off main, pushes it,
// and returns its SHA. age backdates the commit (0 = now).
func (f *overlapFixture) branch(name string, age time.Duration, files ...string) string {
	f.t.Helper()
	f.git(f.work, "", "checkout", "-q", "-B", name, "main")
	edits := map[string]string{}
	for _, file := range files {
		edits[file] = "edited on " + name + "\n"
	}
	f.write(edits)
	f.git(f.work, "", "add", ".")
	date := ""
	if age > 0 {
		date = fmt.Sprintf("@%d +0000", time.Now().Add(-age).Unix())
	}
	f.git(f.work, date, "commit", "-qm", name)
	f.git(f.work, "", "push", "-q", "-f", f.bare, name+":refs/heads/"+name)
	return f.git(f.work, "", "rev-parse", "HEAD")
}

func TestCurrentOverlaps_reportsBranchesSharingFiles(t *testing.T) {
	f := newOverlapFixture(t)
	shaA := f.branch("a", 0, "a.txt")
	shaB := f.branch("b", 0, "a.txt", "b.txt")
	f.branch("c", 0, "c.txt")

	got, err := CurrentOverlaps(f.bare)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want exactly the a/b pair, got %+v", got)
	}
	pair := []string{got[0].Ref, got[0].OtherRef}
	if !(reflect.DeepEqual(pair, []string{"refs/heads/a", "refs/heads/b"}) || reflect.DeepEqual(pair, []string{"refs/heads/b", "refs/heads/a"})) {
		t.Errorf("pair = %v, want a and b", pair)
	}
	if !reflect.DeepEqual(got[0].Files, []string{"a.txt"}) {
		t.Errorf("files = %v, want [a.txt]", got[0].Files)
	}
	shas := map[string]string{got[0].Ref: got[0].SHA, got[0].OtherRef: got[0].OtherSHA}
	if shas["refs/heads/a"] != shaA || shas["refs/heads/b"] != shaB {
		t.Errorf("each branch should carry its own commit: got %v, want a=%s b=%s", shas, shaA, shaB)
	}
}

// A branch already merged into main has no changes left of its own, and an idle
// branch past overlapMaxAge is not open work: neither may produce an overlap.
func TestCurrentOverlaps_ignoresMergedAndIdleBranches(t *testing.T) {
	f := newOverlapFixture(t)
	f.branch("merged", 0, "a.txt")
	f.git(f.work, "", "push", "-q", "-f", f.bare, "merged:refs/heads/main")
	f.branch("idle", 30*24*time.Hour, "a.txt")
	f.branch("fresh", 0, "a.txt")

	got, err := CurrentOverlaps(f.bare)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("merged and idle branches must not overlap, got %+v", got)
	}
}

func TestCurrentOverlaps_capsBranchesNewestFirst(t *testing.T) {
	prev := overlapMaxBranches
	overlapMaxBranches = 2
	t.Cleanup(func() { overlapMaxBranches = prev })

	f := newOverlapFixture(t)
	f.branch("oldest", 3*time.Hour, "a.txt")
	f.branch("middle", 2*time.Hour, "a.txt")
	f.branch("newest", 1*time.Hour, "a.txt")

	got, err := CurrentOverlaps(f.bare)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("with a cap of 2 only one pair can exist, got %+v", got)
	}
	for _, ref := range []string{got[0].Ref, got[0].OtherRef} {
		if ref == "refs/heads/oldest" {
			t.Errorf("the oldest branch should fall outside the cap, got %+v", got[0])
		}
	}
}

func TestCurrentOverlaps_noBaselineIsEmpty(t *testing.T) {
	f := newOverlapFixture(t)
	f.branch("a", 0, "a.txt")
	f.branch("b", 0, "a.txt")
	f.git(f.bare, "", "symbolic-ref", "HEAD", "refs/heads/does-not-exist")

	got, err := CurrentOverlaps(f.bare)
	if err != nil || len(got) != 0 {
		t.Errorf("a dangling HEAD has no baseline: want no overlaps and no error, got %+v, %v", got, err)
	}
}

func TestPushOverlaps_comparesPushedBranchWithOthers(t *testing.T) {
	f := newOverlapFixture(t)
	otherSHA := f.branch("other", 0, "b.txt", "c.txt")
	sha := f.branch("mine", 0, "b.txt")

	got, err := PushOverlaps(f.bare, []RefUpdate{{OldRev: zeroRev, NewRev: sha, Name: "refs/heads/mine"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []Overlap{{Ref: "refs/heads/mine", OtherRef: "refs/heads/other", Files: []string{"b.txt"}, SHA: sha, OtherSHA: otherSHA}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestPushOverlaps_skipsDefaultBranchDeletesAndTags(t *testing.T) {
	f := newOverlapFixture(t)
	f.branch("other", 0, "a.txt")
	sha := f.branch("mine", 0, "a.txt")

	got, err := PushOverlaps(f.bare, []RefUpdate{
		{OldRev: zeroRev, NewRev: sha, Name: "refs/heads/main"},
		{OldRev: sha, NewRev: zeroRev, Name: "refs/heads/mine"},
		{OldRev: zeroRev, NewRev: sha, Name: "refs/tags/v1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("default branch, deletes and tags must not be checked, got %+v", got)
	}
}

func TestIntersectFiles_capsAndSorts(t *testing.T) {
	prev := overlapMaxFiles
	overlapMaxFiles = 2
	t.Cleanup(func() { overlapMaxFiles = prev })

	got := intersectFiles([]string{"z", "y", "x", "only-a"}, []string{"x", "y", "z", "only-b"})
	if !reflect.DeepEqual(got, []string{"x", "y"}) {
		t.Errorf("got %v, want [x y]", got)
	}
}
