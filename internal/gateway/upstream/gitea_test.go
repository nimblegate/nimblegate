// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package upstream

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newGiteaTest spins up a single-handler httptest.Server, returns the
// adapter pre-configured to talk to it, plus the server for cleanup.
func newGiteaTest(t *testing.T, handler http.HandlerFunc) (*GiteaAdapter, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return NewGiteaAdapter(srv.URL+"/you/nimblegate", "fake-token"), srv
}

func TestGitea_Name(t *testing.T) {
	a := NewGiteaAdapter("https://gitea.example.com/x/y", "tok")
	if got := a.Name(); got != "gitea" {
		t.Errorf("Name() = %q, want %q", got, "gitea")
	}
}

func TestGitea_ReadPRPeople_ToleratesReviewers404(t *testing.T) {
	// Several Gitea versions lack the GitHub-style /requested_reviewers
	// endpoint and 404 it. Reviewers are optional, so ReadPRPeople must still
	// return the assignees + nil error rather than sinking the whole delivery.
	adapter, _ := newGiteaTest(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/you/nimblegate/pulls/42":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"assignees":[{"login":"alice"}]}`)
		case "/api/v1/repos/you/nimblegate/pulls/42/requested_reviewers":
			w.WriteHeader(http.StatusNotFound)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})
	people, err := adapter.ReadPRPeople(context.Background(), &PullRequest{Number: 42})
	if err != nil {
		t.Fatalf("ReadPRPeople should tolerate a reviewers 404, got: %v", err)
	}
	if len(people.Assignees) != 1 || people.Assignees[0] != "alice" {
		t.Errorf("Assignees = %v, want [alice]", people.Assignees)
	}
	if len(people.Reviewers) != 0 {
		t.Errorf("Reviewers = %v, want empty", people.Reviewers)
	}
}

func TestGitea_FindPRForRef_Match(t *testing.T) {
	adapter, _ := newGiteaTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/api/v1/repos/you/nimblegate/pulls" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("state"); got != "open" {
			t.Errorf("state query = %s", got)
		}
		if got := r.Header.Get("Authorization"); got != "token fake-token" {
			t.Errorf("auth = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[
			{"number":7,"html_url":"https://gitea.example.com/you/nimblegate/pulls/7","head":{"ref":"other"}},
			{"number":42,"html_url":"https://gitea.example.com/you/nimblegate/pulls/42","head":{"ref":"feat-x"},"created_at":"2026-06-01T10:00:00Z"}
		]`)
	})

	pr, err := adapter.FindPRForRef(context.Background(), "you/nimblegate", "refs/heads/feat-x")
	if err != nil {
		t.Fatalf("FindPRForRef: %v", err)
	}
	if pr == nil {
		t.Fatal("expected PR, got nil")
	}
	if pr.Number != 42 {
		t.Errorf("Number = %d, want 42", pr.Number)
	}
	if pr.URL != "https://gitea.example.com/you/nimblegate/pulls/42" {
		t.Errorf("URL = %s", pr.URL)
	}
	if pr.Ref != "refs/heads/feat-x" {
		t.Errorf("Ref = %s", pr.Ref)
	}
}

func TestGitea_FindPRForRef_NoMatch(t *testing.T) {
	adapter, _ := newGiteaTest(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `[{"number":1,"head":{"ref":"main"}}]`)
	})

	pr, err := adapter.FindPRForRef(context.Background(), "you/nimblegate", "refs/heads/feat-x")
	if err != nil {
		t.Fatalf("FindPRForRef: %v", err)
	}
	if pr != nil {
		t.Errorf("expected nil PR, got %+v", pr)
	}
}

func TestGitea_FindPRForRef_EmptyList(t *testing.T) {
	adapter, _ := newGiteaTest(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `[]`)
	})
	pr, err := adapter.FindPRForRef(context.Background(), "you/nimblegate", "refs/heads/main")
	if err != nil || pr != nil {
		t.Errorf("expected (nil, nil), got pr=%+v err=%v", pr, err)
	}
}

func TestGitea_ReadPRPeople(t *testing.T) {
	adapter, _ := newGiteaTest(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/you/nimblegate/pulls/42":
			_, _ = io.WriteString(w, `{"number":42,"assignees":[{"login":"alice"},{"login":"bob"}]}`)
		case "/api/v1/repos/you/nimblegate/pulls/42/requested_reviewers":
			_, _ = io.WriteString(w, `[{"login":"carol"},{"login":"dave"}]`)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	})

	people, err := adapter.ReadPRPeople(context.Background(), &PullRequest{Number: 42})
	if err != nil {
		t.Fatalf("ReadPRPeople: %v", err)
	}
	if strings.Join(people.Assignees, ",") != "alice,bob" {
		t.Errorf("Assignees = %v", people.Assignees)
	}
	if strings.Join(people.Reviewers, ",") != "carol,dave" {
		t.Errorf("Reviewers = %v", people.Reviewers)
	}
}

func TestGitea_ReadPRPeople_Empty(t *testing.T) {
	adapter, _ := newGiteaTest(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/repos/you/nimblegate/pulls/9":
			_, _ = io.WriteString(w, `{"number":9,"assignees":null}`)
		case "/api/v1/repos/you/nimblegate/pulls/9/requested_reviewers":
			_, _ = io.WriteString(w, `[]`)
		}
	})
	people, err := adapter.ReadPRPeople(context.Background(), &PullRequest{Number: 9})
	if err != nil {
		t.Fatalf("ReadPRPeople: %v", err)
	}
	if len(people.Assignees) != 0 || len(people.Reviewers) != 0 {
		t.Errorf("expected empty, got %+v", people)
	}
}

func TestGitea_FindStickyComment_Found(t *testing.T) {
	adapter, _ := newGiteaTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/you/nimblegate/issues/comments/555" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"id":555,"body":"sticky body","html_url":"https://gitea.example.com/you/nimblegate/pulls/42#issuecomment-555"}`)
	})

	c, err := adapter.FindStickyComment(context.Background(), &PullRequest{Number: 42}, "555")
	if err != nil {
		t.Fatalf("FindStickyComment: %v", err)
	}
	if c == nil || c.ID != "555" || c.Body != "sticky body" {
		t.Errorf("got %+v", c)
	}
}

func TestGitea_FindStickyComment_404(t *testing.T) {
	adapter, _ := newGiteaTest(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
	})

	c, err := adapter.FindStickyComment(context.Background(), &PullRequest{Number: 42}, "999")
	if err != nil {
		t.Fatalf("expected nil err on 404, got %v", err)
	}
	if c != nil {
		t.Errorf("expected nil comment on 404, got %+v", c)
	}
}

func TestGitea_ScanForMarker_FoundNewest(t *testing.T) {
	adapter, _ := newGiteaTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/repos/you/nimblegate/issues/42/comments" {
			t.Errorf("path = %s", r.URL.Path)
		}
		// Server order: oldest → newest. ScanForMarker walks newest-first
		// and should pick id=3 (the most recent containing the marker).
		_, _ = io.WriteString(w, `[
			{"id":1,"body":"unrelated"},
			{"id":2,"body":"<!-- nimblegate-data --> older sticky"},
			{"id":3,"body":"<!-- nimblegate-data --> newer sticky"}
		]`)
	})

	c, err := adapter.ScanForMarker(context.Background(), &PullRequest{Number: 42}, "<!-- nimblegate-data -->")
	if err != nil {
		t.Fatalf("ScanForMarker: %v", err)
	}
	if c == nil || c.ID != "3" {
		t.Errorf("expected id=3, got %+v", c)
	}
}

func TestGitea_ScanForMarker_NoMatch(t *testing.T) {
	adapter, _ := newGiteaTest(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `[{"id":1,"body":"nothing here"}]`)
	})
	c, err := adapter.ScanForMarker(context.Background(), &PullRequest{Number: 42}, "<!-- nimblegate-data -->")
	if err != nil || c != nil {
		t.Errorf("expected (nil, nil), got c=%+v err=%v", c, err)
	}
}

func TestGitea_CreateComment(t *testing.T) {
	adapter, _ := newGiteaTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/api/v1/repos/you/nimblegate/issues/42/comments" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type = %s", ct)
		}
		var got map[string]string
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if got["body"] != "hello world" {
			t.Errorf("posted body = %q", got["body"])
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"id":777,"body":"hello world","html_url":"https://gitea.example.com/you/nimblegate/pulls/42#issuecomment-777"}`)
	})

	c, err := adapter.CreateComment(context.Background(), &PullRequest{Number: 42}, "hello world")
	if err != nil {
		t.Fatalf("CreateComment: %v", err)
	}
	if c.ID != "777" {
		t.Errorf("ID = %s, want 777", c.ID)
	}
	if c.URL != "https://gitea.example.com/you/nimblegate/pulls/42#issuecomment-777" {
		t.Errorf("URL = %s", c.URL)
	}
	if c.Body != "hello world" {
		t.Errorf("Body = %q", c.Body)
	}
}

func TestGitea_UpdateComment_OK(t *testing.T) {
	adapter, _ := newGiteaTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" {
			t.Errorf("method = %s, want PATCH", r.Method)
		}
		if r.URL.Path != "/api/v1/repos/you/nimblegate/issues/comments/555" {
			t.Errorf("path = %s", r.URL.Path)
		}
		var got map[string]string
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got["body"] != "updated" {
			t.Errorf("posted body = %q", got["body"])
		}
		_, _ = io.WriteString(w, `{"id":555,"body":"updated"}`)
	})

	err := adapter.UpdateComment(context.Background(), &Comment{ID: "555"}, "updated")
	if err != nil {
		t.Fatalf("UpdateComment: %v", err)
	}
}

func TestGitea_UpdateComment_404IsTransient(t *testing.T) {
	adapter, _ := newGiteaTest(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
	})

	err := adapter.UpdateComment(context.Background(), &Comment{ID: "999"}, "x")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrTransient) {
		t.Errorf("expected ErrTransient, got %v", err)
	}
}

func TestGitea_ErrorClassification(t *testing.T) {
	cases := []struct {
		name   string
		status int
		want   error
	}{
		{"500_transient", http.StatusInternalServerError, ErrTransient},
		{"502_transient", http.StatusBadGateway, ErrTransient},
		{"429_transient", http.StatusTooManyRequests, ErrTransient},
		{"403_permanent", http.StatusForbidden, ErrPermanent},
		{"401_permanent", http.StatusUnauthorized, ErrPermanent},
		{"422_permanent", http.StatusUnprocessableEntity, ErrPermanent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			adapter, _ := newGiteaTest(t, func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "boom", tc.status)
			})
			_, err := adapter.FindPRForRef(context.Background(), "you/nimblegate", "refs/heads/main")
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, tc.want) {
				t.Errorf("status %d: want %v, got %v", tc.status, tc.want, err)
			}
		})
	}
}

func TestGitea_TokenNotInErrors(t *testing.T) {
	adapter, _ := newGiteaTest(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusForbidden)
	})
	// Use a distinctive secret so any leak shows up clearly.
	adapter.token = "secret-pat-do-not-leak"

	_, err := adapter.FindPRForRef(context.Background(), "you/nimblegate", "refs/heads/main")
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "secret-pat-do-not-leak") {
		t.Errorf("token leaked into error: %v", err)
	}
}

func TestGitea_SplitRepoURL(t *testing.T) {
	cases := []struct {
		in       string
		wantAPI  string
		wantRepo string
	}{
		{"https://gitea.example.com/you/nimblegate", "https://gitea.example.com", "you/nimblegate"},
		{"https://gitea.example.com/you/nimblegate/", "https://gitea.example.com", "you/nimblegate"},
		{"https://gitea.example.com/you/nimblegate.git", "https://gitea.example.com", "you/nimblegate"},
		{"http://192.0.2.20:3000/you/gw-test.git", "http://192.0.2.20:3000", "you/gw-test"},
		{"http://localhost:3000/foo/bar", "http://localhost:3000", "foo/bar"},
		{"https://gitea.example.com/just-owner", "https://gitea.example.com", ""},
		{"not a url", "", ""},
	}
	for _, tc := range cases {
		gotAPI, gotRepo := splitRepoURL(tc.in)
		if gotAPI != tc.wantAPI || gotRepo != tc.wantRepo {
			t.Errorf("splitRepoURL(%q) = (%q, %q), want (%q, %q)", tc.in, gotAPI, gotRepo, tc.wantAPI, tc.wantRepo)
		}
	}
}
