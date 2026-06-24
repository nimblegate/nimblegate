// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package notification

import "testing"

func TestListLoopsForRef(t *testing.T) {
	root := t.TempDir()
	// Two open PRs: one on the ref that just got a clean push, one on another.
	if err := WritePRState(root, "gw-test", 1, PRState{
		PRNumber: 1, Repo: "gw-test", Ref: "refs/heads/fix-demo",
		Loop:          LoopCounters{AttemptCount: 2, MaxAttempts: 5},
		StickyComment: StickyCommentRef{ID: "c-100"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := WritePRState(root, "gw-test", 2, PRState{
		PRNumber: 2, Repo: "gw-test", Ref: "refs/heads/other",
		Loop: LoopCounters{AttemptCount: 1, MaxAttempts: 5},
	}); err != nil {
		t.Fatal(err)
	}

	got, err := ListLoopsForRef(root, "gw-test", "refs/heads/fix-demo")
	if err != nil {
		t.Fatalf("ListLoopsForRef: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("matched %d loops, want 1: %+v", len(got), got)
	}
	if got[0].PRNumber != 1 || got[0].StickyComment.ID != "c-100" {
		t.Errorf("matched %+v, want PR 1 with sticky c-100", got[0])
	}

	// A ref with no loops returns empty, no error.
	if got, err := ListLoopsForRef(root, "gw-test", "refs/heads/nope"); err != nil || len(got) != 0 {
		t.Errorf("ListLoopsForRef(no match) = (%v, %v), want (empty, nil)", got, err)
	}
}
