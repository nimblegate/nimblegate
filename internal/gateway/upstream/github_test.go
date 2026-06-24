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

// newGitHubTest spins up a single-handler httptest.Server and returns an
// adapter pre-configured to talk to it. We override apiBase to point at
// the test server so endpoint paths look exactly like GitHub
// (/repos/owner/repo/...) - splitGitHubRepoURL is exercised separately.
func newGitHubTest(t *testing.T, handler http.HandlerFunc) (*GitHubAdapter, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	a := NewGitHubAdapter("https://github.com/you/nimblegate", "fake-token")
	a.apiBase = srv.URL
	return a, srv
}

func TestGitHub_Name(t *testing.T) {
	a := NewGitHubAdapter("https://github.com/x/y", "tok")
	if got := a.Name(); got != "github" {
		t.Errorf("Name() = %q, want %q", got, "github")
	}
}

func TestGitHub_FindPRForRef_Open(t *testing.T) {
	adapter, _ := newGitHubTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/repos/you/nimblegate/pulls" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("state"); got != "open" {
			t.Errorf("state query = %s", got)
		}
		// GitHub's head-ref filter is `owner:branch`.
		if got := r.URL.Query().Get("head"); got != "you:feat-x" {
			t.Errorf("head query = %q, want you:feat-x", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer fake-token" {
			t.Errorf("auth = %q, want Bearer fake-token", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `[
			{"number":42,"html_url":"https://github.com/you/nimblegate/pull/42","head":{"ref":"feat-x"},"created_at":"2026-06-01T10:00:00Z"}
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
	if pr.URL != "https://github.com/you/nimblegate/pull/42" {
		t.Errorf("URL = %s", pr.URL)
	}
	if pr.Ref != "refs/heads/feat-x" {
		t.Errorf("Ref = %s", pr.Ref)
	}
}

func TestGitHub_FindPRForRef_NoOpenPR(t *testing.T) {
	adapter, _ := newGitHubTest(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `[]`)
	})

	pr, err := adapter.FindPRForRef(context.Background(), "you/nimblegate", "refs/heads/feat-x")
	if err != nil || pr != nil {
		t.Errorf("expected (nil, nil), got pr=%+v err=%v", pr, err)
	}
}

func TestGitHub_ReadPRPeople(t *testing.T) {
	// GitHub-specific: assignees AND requested_reviewers come from the
	// SAME PR payload - only ONE HTTP call expected.
	var calls int
	adapter, _ := newGitHubTest(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/repos/you/nimblegate/pulls/42" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{
			"number": 42,
			"assignees": [{"login":"alice"},{"login":"bob"}],
			"requested_reviewers": [{"login":"carol"},{"login":"dave"}]
		}`)
	})

	people, err := adapter.ReadPRPeople(context.Background(), &PullRequest{Number: 42})
	if err != nil {
		t.Fatalf("ReadPRPeople: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected exactly 1 HTTP call, got %d", calls)
	}
	if strings.Join(people.Assignees, ",") != "alice,bob" {
		t.Errorf("Assignees = %v", people.Assignees)
	}
	if strings.Join(people.Reviewers, ",") != "carol,dave" {
		t.Errorf("Reviewers = %v", people.Reviewers)
	}
}

func TestGitHub_ReadPRPeople_Empty(t *testing.T) {
	adapter, _ := newGitHubTest(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"number":9,"assignees":null,"requested_reviewers":[]}`)
	})
	people, err := adapter.ReadPRPeople(context.Background(), &PullRequest{Number: 9})
	if err != nil {
		t.Fatalf("ReadPRPeople: %v", err)
	}
	if len(people.Assignees) != 0 || len(people.Reviewers) != 0 {
		t.Errorf("expected empty, got %+v", people)
	}
}

func TestGitHub_FindStickyComment_Success(t *testing.T) {
	adapter, _ := newGitHubTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/you/nimblegate/issues/comments/555" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"id":555,"body":"sticky body","html_url":"https://github.com/you/nimblegate/pull/42#issuecomment-555"}`)
	})

	c, err := adapter.FindStickyComment(context.Background(), &PullRequest{Number: 42}, "555")
	if err != nil {
		t.Fatalf("FindStickyComment: %v", err)
	}
	if c == nil || c.ID != "555" || c.Body != "sticky body" {
		t.Errorf("got %+v", c)
	}
}

func TestGitHub_FindStickyComment_404ReturnsNilNil(t *testing.T) {
	adapter, _ := newGitHubTest(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	})

	c, err := adapter.FindStickyComment(context.Background(), &PullRequest{Number: 42}, "999")
	if err != nil {
		t.Fatalf("expected nil err on 404, got %v", err)
	}
	if c != nil {
		t.Errorf("expected nil comment on 404, got %+v", c)
	}
}

func TestGitHub_ScanForMarker_NewestFirst(t *testing.T) {
	adapter, _ := newGitHubTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/you/nimblegate/issues/42/comments" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("per_page"); got != "100" {
			t.Errorf("per_page = %s, want 100", got)
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

func TestGitHub_ScanForMarker_NoMatch(t *testing.T) {
	adapter, _ := newGitHubTest(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `[{"id":1,"body":"nothing here"}]`)
	})
	c, err := adapter.ScanForMarker(context.Background(), &PullRequest{Number: 42}, "<!-- nimblegate-data -->")
	if err != nil || c != nil {
		t.Errorf("expected (nil, nil), got c=%+v err=%v", c, err)
	}
}

func TestGitHub_CreateComment_PostsBody(t *testing.T) {
	adapter, _ := newGitHubTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/repos/you/nimblegate/issues/42/comments" {
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
		_, _ = io.WriteString(w, `{"id":777,"body":"hello world","html_url":"https://github.com/you/nimblegate/pull/42#issuecomment-777"}`)
	})

	c, err := adapter.CreateComment(context.Background(), &PullRequest{Number: 42}, "hello world")
	if err != nil {
		t.Fatalf("CreateComment: %v", err)
	}
	if c.ID != "777" {
		t.Errorf("ID = %s, want 777", c.ID)
	}
	if c.URL != "https://github.com/you/nimblegate/pull/42#issuecomment-777" {
		t.Errorf("URL = %s", c.URL)
	}
	if c.Body != "hello world" {
		t.Errorf("Body = %q", c.Body)
	}
}

func TestGitHub_UpdateComment_Success(t *testing.T) {
	adapter, _ := newGitHubTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" {
			t.Errorf("method = %s, want PATCH", r.Method)
		}
		if r.URL.Path != "/repos/you/nimblegate/issues/comments/555" {
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

	if err := adapter.UpdateComment(context.Background(), &Comment{ID: "555"}, "updated"); err != nil {
		t.Fatalf("UpdateComment: %v", err)
	}
}

func TestGitHub_UpdateComment_404IsTransient(t *testing.T) {
	adapter, _ := newGitHubTest(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
	})

	err := adapter.UpdateComment(context.Background(), &Comment{ID: "999"}, "x")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrTransient) {
		t.Errorf("expected ErrTransient, got %v", err)
	}
}

func TestGitHub_ErrorClassification(t *testing.T) {
	cases := []struct {
		name   string
		status int
		want   error
	}{
		{"500_transient", http.StatusInternalServerError, ErrTransient},
		{"502_transient", http.StatusBadGateway, ErrTransient},
		{"429_transient", http.StatusTooManyRequests, ErrTransient},
		{"401_permanent", http.StatusUnauthorized, ErrPermanent},
		{"403_permanent", http.StatusForbidden, ErrPermanent},
		{"422_permanent", http.StatusUnprocessableEntity, ErrPermanent},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			adapter, _ := newGitHubTest(t, func(w http.ResponseWriter, r *http.Request) {
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

func TestGitHub_PrimaryRateLimit_403WithRemainingZero_IsTransient(t *testing.T) {
	adapter, _ := newGitHubTest(t, func(w http.ResponseWriter, r *http.Request) {
		// GitHub's primary rate limit is signalled by a 403 with
		// X-RateLimit-Remaining: 0 (NOT a 429). It is retryable.
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.Header().Set("X-RateLimit-Reset", "1717000000")
		http.Error(w, `{"message":"API rate limit exceeded"}`, http.StatusForbidden)
	})

	_, err := adapter.FindPRForRef(context.Background(), "you/nimblegate", "refs/heads/main")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrTransient) {
		t.Errorf("403 + X-RateLimit-Remaining: 0 should be ErrTransient, got %v", err)
	}
	if errors.Is(err, ErrPermanent) {
		t.Errorf("403 + X-RateLimit-Remaining: 0 should NOT be ErrPermanent, got %v", err)
	}
}

func TestGitHub_TokenNotInErrors(t *testing.T) {
	adapter, _ := newGitHubTest(t, func(w http.ResponseWriter, r *http.Request) {
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

func TestGitHub_Headers_UserAgent_Bearer_Accept(t *testing.T) {
	// Every request must carry Authorization: Bearer, Accept:
	// application/vnd.github+json, and User-Agent: nimblegate/v0.1.0.
	// Exercise once per HTTP method we use (GET, POST, PATCH).
	type seen struct {
		auth, accept, ua string
	}
	captured := map[string]seen{}

	adapter, _ := newGitHubTest(t, func(w http.ResponseWriter, r *http.Request) {
		captured[r.Method] = seen{
			auth:   r.Header.Get("Authorization"),
			accept: r.Header.Get("Accept"),
			ua:     r.Header.Get("User-Agent"),
		}
		switch r.Method {
		case "POST":
			w.WriteHeader(http.StatusCreated)
			_, _ = io.WriteString(w, `{"id":1,"body":"x"}`)
		case "PATCH":
			_, _ = io.WriteString(w, `{"id":1,"body":"x"}`)
		default:
			_, _ = io.WriteString(w, `[]`)
		}
	})

	_, _ = adapter.FindPRForRef(context.Background(), "you/nimblegate", "refs/heads/main")
	_, _ = adapter.CreateComment(context.Background(), &PullRequest{Number: 1}, "x")
	_ = adapter.UpdateComment(context.Background(), &Comment{ID: "1"}, "x")

	for _, method := range []string{"GET", "POST", "PATCH"} {
		s, ok := captured[method]
		if !ok {
			t.Errorf("no %s request captured", method)
			continue
		}
		if s.auth != "Bearer fake-token" {
			t.Errorf("%s Authorization = %q, want Bearer fake-token", method, s.auth)
		}
		if s.accept != "application/vnd.github+json" {
			t.Errorf("%s Accept = %q, want application/vnd.github+json", method, s.accept)
		}
		if s.ua != "nimblegate/v0.1.0" {
			t.Errorf("%s User-Agent = %q, want nimblegate/v0.1.0", method, s.ua)
		}
	}
}

func TestGitHub_SplitRepoURL(t *testing.T) {
	cases := []struct {
		in       string
		wantAPI  string
		wantRepo string
	}{
		// github.com → the well-known api.github.com host.
		{"https://github.com/you/nimblegate", "https://api.github.com", "you/nimblegate"},
		{"https://github.com/you/nimblegate/", "https://api.github.com", "you/nimblegate"},
		{"https://github.com/you/nimblegate.git", "https://api.github.com", "you/nimblegate"},
		{"https://www.github.com/you/nimblegate", "https://api.github.com", "you/nimblegate"},
		// GitHub Enterprise Server → https://<host>/api/v3.
		{"https://ghe.example.com/you/nimblegate", "https://ghe.example.com/api/v3", "you/nimblegate"},
		{"http://localhost:3000/foo/bar", "http://localhost:3000/api/v3", "foo/bar"},
		// Single-segment paths produce empty repo (handled at request time).
		{"https://github.com/just-owner", "https://api.github.com", ""},
		{"not a url", "", ""},
	}
	for _, tc := range cases {
		gotAPI, gotRepo := splitGitHubRepoURL(tc.in)
		if gotAPI != tc.wantAPI || gotRepo != tc.wantRepo {
			t.Errorf("splitGitHubRepoURL(%q) = (%q, %q), want (%q, %q)", tc.in, gotAPI, gotRepo, tc.wantAPI, tc.wantRepo)
		}
	}
}
