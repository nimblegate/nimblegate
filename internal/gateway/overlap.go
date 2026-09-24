// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Overlap is a pair of open branches that change at least one of the same
// files, each measured against its merge-base with the default branch.
type Overlap struct {
	Ref      string   `json:"ref"`
	OtherRef string   `json:"other_ref"`
	Files    []string `json:"files"`
	SHA      string   `json:"sha,omitempty"`       // Ref's commit when the overlap was seen; empty on lines before 2026-09-25
	OtherSHA string   `json:"other_sha,omitempty"` // OtherRef's commit at the same moment
}

// Bounds that keep the check cheap enough to run inside pre-receive. Idle
// branches rarely collide with live work, and a repo with hundreds of branches
// must not turn every push into hundreds of diffs. Vars so tests can shrink them.
var (
	overlapMaxBranches = 50
	overlapMaxAge      = 14 * 24 * time.Hour
	overlapMaxFiles    = 20
)

const overlapTimeout = 2 * time.Second

type openBranch struct {
	ref string
	sha string
}

// PushOverlaps reports, for each branch in the push, the other open branches
// that change any of the same files. The default branch is the baseline, so it
// is neither checked nor compared against. Deletes and non-branch refs are
// skipped.
func PushOverlaps(bareDir string, refs []RefUpdate) ([]Overlap, error) {
	ctx, cancel := context.WithTimeout(context.Background(), overlapTimeout)
	defer cancel()
	base, branches, err := openBranches(ctx, bareDir)
	if err != nil || base == "" {
		return nil, err
	}
	cache := map[string][]string{}
	changed := func(sha string) ([]string, error) {
		if files, ok := cache[sha]; ok {
			return files, nil
		}
		files, err := changedFiles(ctx, bareDir, base, sha)
		cache[sha] = files
		return files, err
	}
	var out []Overlap
	for _, r := range refs {
		if r.IsDelete() || !strings.HasPrefix(r.Name, "refs/heads/") || r.Name == base {
			continue
		}
		mine, err := changed(r.NewRev)
		if err != nil {
			return nil, err
		}
		if len(mine) == 0 {
			continue
		}
		for _, b := range branches {
			if b.ref == r.Name {
				continue
			}
			theirs, err := changed(b.sha)
			if err != nil {
				return nil, err
			}
			if files := intersectFiles(mine, theirs); len(files) > 0 {
				out = append(out, Overlap{Ref: r.Name, OtherRef: b.ref, Files: files, SHA: r.NewRev, OtherSHA: b.sha})
			}
		}
	}
	return out, nil
}

// CurrentOverlaps reports every pair of open branches in the repo that change
// any of the same files right now. Used by the dashboard, which reads it live
// rather than from the audit log.
func CurrentOverlaps(bareDir string) ([]Overlap, error) {
	ctx, cancel := context.WithTimeout(context.Background(), overlapTimeout)
	defer cancel()
	base, branches, err := openBranches(ctx, bareDir)
	if err != nil || base == "" {
		return nil, err
	}
	changed := make([][]string, len(branches))
	for i, b := range branches {
		if changed[i], err = changedFiles(ctx, bareDir, base, b.sha); err != nil {
			return nil, err
		}
	}
	var out []Overlap
	for i := range branches {
		for j := i + 1; j < len(branches); j++ {
			if files := intersectFiles(changed[i], changed[j]); len(files) > 0 {
				out = append(out, Overlap{Ref: branches[i].ref, OtherRef: branches[j].ref, Files: files, SHA: branches[i].sha, OtherSHA: branches[j].sha})
			}
		}
	}
	return out, nil
}

// openBranches returns the default branch ref and the non-default branches
// committed to within overlapMaxAge, newest first, capped at overlapMaxBranches.
// An unset or dangling HEAD returns an empty base: with no baseline there is
// nothing to measure a branch's changes against.
func openBranches(ctx context.Context, bareDir string) (string, []openBranch, error) {
	out, err := gitBareContext(ctx, bareDir, "symbolic-ref", "-q", "HEAD").Output()
	if err != nil {
		return "", nil, ctx.Err()
	}
	base := strings.TrimSpace(string(out))
	if gitBareContext(ctx, bareDir, "rev-parse", "-q", "--verify", base+"^{commit}").Run() != nil {
		return "", nil, ctx.Err()
	}
	out, err = gitBareContext(ctx, bareDir, "for-each-ref", "--sort=-committerdate",
		"--format=%(refname)%09%(objectname)%09%(committerdate:unix)", "refs/heads/").Output()
	if err != nil {
		return "", nil, err
	}
	cutoff := time.Now().Add(-overlapMaxAge).Unix()
	var branches []openBranch
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 3 || f[0] == base {
			continue
		}
		if ts, _ := strconv.ParseInt(f[2], 10, 64); ts < cutoff {
			break // sorted newest first, so every later branch is older still
		}
		branches = append(branches, openBranch{ref: f[0], sha: f[1]})
		if len(branches) == overlapMaxBranches {
			break
		}
	}
	return base, branches, nil
}

// changedFiles lists the files tip changes since its merge-base with base.
// Unrelated history (no merge-base) has nothing in common to compare, so it
// reports no files rather than failing the whole check.
func changedFiles(ctx context.Context, bareDir, base, tip string) ([]string, error) {
	mb, err := gitBareContext(ctx, bareDir, "merge-base", base, tip).Output()
	if err != nil {
		return nil, ctx.Err()
	}
	out, err := gitBareContext(ctx, bareDir, "diff", "--name-only", "-z", "--no-renames",
		strings.TrimSpace(string(mb)), tip).Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	var files []string
	for _, f := range strings.Split(string(out), "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	return files, nil
}

func intersectFiles(a, b []string) []string {
	if len(a) == 0 || len(b) == 0 {
		return nil
	}
	set := make(map[string]bool, len(b))
	for _, f := range b {
		set[f] = true
	}
	var out []string
	for _, f := range a {
		if set[f] {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	if len(out) > overlapMaxFiles {
		out = out[:overlapMaxFiles]
	}
	return out
}
