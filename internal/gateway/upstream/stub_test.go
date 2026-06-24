// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package upstream

import (
	"context"
	"strings"
	"testing"
)

func TestStub_CreateThenFindComment(t *testing.T) {
	stub := NewStub()
	stub.AddPR("nimblegate", "refs/heads/main", &PullRequest{Number: 42})

	pr, err := stub.FindPRForRef(context.Background(), "nimblegate", "refs/heads/main")
	if err != nil || pr == nil || pr.Number != 42 {
		t.Fatalf("find PR: err=%v pr=%+v", err, pr)
	}

	created, err := stub.CreateComment(context.Background(), pr, "hello")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	found, err := stub.FindStickyComment(context.Background(), pr, created.ID)
	if err != nil || found == nil || found.Body != "hello" {
		t.Errorf("find sticky: err=%v c=%+v", err, found)
	}
}

func TestStub_FindPRForRef_NoMatch(t *testing.T) {
	stub := NewStub()
	pr, err := stub.FindPRForRef(context.Background(), "unknown", "refs/heads/main")
	if err != nil || pr != nil {
		t.Errorf("no PR on ref should be nil, nil - got err=%v pr=%+v", err, pr)
	}
}

func TestStub_ScanForMarker(t *testing.T) {
	stub := NewStub()
	pr := &PullRequest{Number: 1}
	stub.AddPR("repo", "refs/heads/main", pr)
	_, _ = stub.CreateComment(context.Background(), pr, "first comment, no marker")
	_, _ = stub.CreateComment(context.Background(), pr, "second\n<!-- nimblegate-data: {} -->")

	found, err := stub.ScanForMarker(context.Background(), pr, "<!-- nimblegate-data:")
	if err != nil || found == nil {
		t.Fatalf("scan: err=%v c=%v", err, found)
	}
	if !strings.Contains(found.Body, "nimblegate-data") {
		t.Errorf("scan returned wrong comment: %q", found.Body)
	}
}

func TestStub_UpdateComment(t *testing.T) {
	stub := NewStub()
	pr := &PullRequest{Number: 1}
	stub.AddPR("repo", "refs/heads/main", pr)
	c, _ := stub.CreateComment(context.Background(), pr, "v1")
	if err := stub.UpdateComment(context.Background(), c, "v2"); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, _ := stub.FindStickyComment(context.Background(), pr, c.ID)
	if got.Body != "v2" {
		t.Errorf("body should be v2, got %q", got.Body)
	}
}

func TestStub_ReadPRPeople(t *testing.T) {
	stub := NewStub()
	pr := &PullRequest{Number: 42}
	stub.SetPeople(42, PRPeople{Assignees: []string{"alice"}, Reviewers: []string{"reviewer1"}})
	people, err := stub.ReadPRPeople(context.Background(), pr)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(people.Assignees) != 1 || people.Assignees[0] != "alice" {
		t.Errorf("assignees wrong: %+v", people)
	}
}
