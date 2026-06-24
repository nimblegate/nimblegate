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

// This file is the shared conformance suite for Upstream adapters. The
// contract is: every adapter (gitea, github, future gitlab/bitbucket)
// MUST pass every scenario here identically. The per-scenario handlers
// know about the host-specific URL shapes (gitea's /api/v1/... vs
// github's /repos/...), but the ASSERTIONS only touch the Upstream
// interface - guaranteeing that whatever host is on the other side,
// the orchestrator sees the same behaviour.
//
// Approach: each scenario supplies a pair of per-host http.HandlerFuncs
// plus one assertion that runs against the constructed adapter. The
// suite wires the handler to a fresh httptest.Server and builds an
// adapter pointing at it via the per-host adapterFactory.

// adapterFactory builds a fresh adapter for a given test server. The
// host-specific bits (auth header value, apiBase override for the
// GitHub adapter) live inside the factory - scenarios stay agnostic.
type adapterFactory func(t *testing.T, srv *httptest.Server) Upstream

// scenarioHandlers carries the per-host HandlerFunc each scenario needs.
// One scenario, two handlers - one shape per host. The assert closure is
// shared across both.
type scenarioHandlers struct {
	Gitea  http.HandlerFunc
	GitHub http.HandlerFunc
}

// upstreamScenario is one row of the conformance table.
type upstreamScenario struct {
	name     string
	handlers scenarioHandlers
	assert   func(t *testing.T, adapter Upstream)
}

// sharedScenarios is the 12-scenario conformance contract. Every
// adapter - gitea, github, and any future adapter - must pass every
// row here identically.
var sharedScenarios = []upstreamScenario{
	{
		name: "FindPR_Exists",
		handlers: scenarioHandlers{
			Gitea: func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, `[
					{"number":42,"html_url":"https://host/you/nimblegate/pulls/42","head":{"ref":"feat-x"}}
				]`)
			},
			GitHub: func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, `[
					{"number":42,"html_url":"https://host/you/nimblegate/pull/42","head":{"ref":"feat-x"}}
				]`)
			},
		},
		assert: func(t *testing.T, adapter Upstream) {
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
			if pr.URL == "" {
				t.Errorf("URL is empty")
			}
			if pr.Ref != "refs/heads/feat-x" {
				t.Errorf("Ref = %s", pr.Ref)
			}
		},
	},
	{
		name: "FindPR_NotFound",
		handlers: scenarioHandlers{
			Gitea:  func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, `[]`) },
			GitHub: func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, `[]`) },
		},
		assert: func(t *testing.T, adapter Upstream) {
			pr, err := adapter.FindPRForRef(context.Background(), "you/nimblegate", "refs/heads/feat-x")
			if err != nil {
				t.Fatalf("FindPRForRef: %v", err)
			}
			if pr != nil {
				t.Errorf("expected nil PR, got %+v", pr)
			}
		},
	},
	{
		name: "ReadPRPeople_Multiple",
		handlers: scenarioHandlers{
			// Gitea uses TWO endpoints (PR + /requested_reviewers).
			Gitea: func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/requested_reviewers"):
					_, _ = io.WriteString(w, `[{"login":"carol"},{"login":"dave"}]`)
				default:
					_, _ = io.WriteString(w, `{"number":42,"assignees":[{"login":"alice"},{"login":"bob"}]}`)
				}
			},
			// GitHub returns BOTH in the PR payload.
			GitHub: func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, `{
					"number":42,
					"assignees":[{"login":"alice"},{"login":"bob"}],
					"requested_reviewers":[{"login":"carol"},{"login":"dave"}]
				}`)
			},
		},
		assert: func(t *testing.T, adapter Upstream) {
			people, err := adapter.ReadPRPeople(context.Background(), &PullRequest{Number: 42})
			if err != nil {
				t.Fatalf("ReadPRPeople: %v", err)
			}
			if got := strings.Join(people.Assignees, ","); got != "alice,bob" {
				t.Errorf("Assignees = %v, want [alice bob]", people.Assignees)
			}
			if got := strings.Join(people.Reviewers, ","); got != "carol,dave" {
				t.Errorf("Reviewers = %v, want [carol dave]", people.Reviewers)
			}
		},
	},
	{
		name: "ReadPRPeople_None",
		handlers: scenarioHandlers{
			Gitea: func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.HasSuffix(r.URL.Path, "/requested_reviewers"):
					_, _ = io.WriteString(w, `[]`)
				default:
					_, _ = io.WriteString(w, `{"number":9,"assignees":null}`)
				}
			},
			GitHub: func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, `{"number":9,"assignees":null,"requested_reviewers":[]}`)
			},
		},
		assert: func(t *testing.T, adapter Upstream) {
			people, err := adapter.ReadPRPeople(context.Background(), &PullRequest{Number: 9})
			if err != nil {
				t.Fatalf("ReadPRPeople: %v", err)
			}
			if len(people.Assignees) != 0 || len(people.Reviewers) != 0 {
				t.Errorf("expected empty, got %+v", people)
			}
		},
	},
	{
		name: "FindStickyComment_Success",
		handlers: scenarioHandlers{
			Gitea: func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, `{"id":555,"body":"sticky body","html_url":"https://host/you/nimblegate/pulls/42#issuecomment-555"}`)
			},
			GitHub: func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, `{"id":555,"body":"sticky body","html_url":"https://host/you/nimblegate/pull/42#issuecomment-555"}`)
			},
		},
		assert: func(t *testing.T, adapter Upstream) {
			c, err := adapter.FindStickyComment(context.Background(), &PullRequest{Number: 42}, "555")
			if err != nil {
				t.Fatalf("FindStickyComment: %v", err)
			}
			if c == nil {
				t.Fatal("expected comment, got nil")
			}
			if c.ID != "555" {
				t.Errorf("ID = %q, want 555", c.ID)
			}
			if c.Body != "sticky body" {
				t.Errorf("Body = %q", c.Body)
			}
		},
	},
	{
		name: "FindStickyComment_404_ReturnsNilNil",
		handlers: scenarioHandlers{
			Gitea: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
			},
			GitHub: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
			},
		},
		assert: func(t *testing.T, adapter Upstream) {
			c, err := adapter.FindStickyComment(context.Background(), &PullRequest{Number: 42}, "999")
			if err != nil {
				t.Fatalf("expected nil err on 404, got %v", err)
			}
			if c != nil {
				t.Errorf("expected nil comment on 404, got %+v", c)
			}
		},
	},
	{
		name: "ScanForMarker_NewestWins",
		handlers: scenarioHandlers{
			Gitea: func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, `[
					{"id":1,"body":"unrelated"},
					{"id":2,"body":"<!-- nimblegate-data --> older sticky"},
					{"id":3,"body":"<!-- nimblegate-data --> newer sticky"}
				]`)
			},
			GitHub: func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, `[
					{"id":1,"body":"unrelated"},
					{"id":2,"body":"<!-- nimblegate-data --> older sticky"},
					{"id":3,"body":"<!-- nimblegate-data --> newer sticky"}
				]`)
			},
		},
		assert: func(t *testing.T, adapter Upstream) {
			c, err := adapter.ScanForMarker(context.Background(), &PullRequest{Number: 42}, "<!-- nimblegate-data -->")
			if err != nil {
				t.Fatalf("ScanForMarker: %v", err)
			}
			if c == nil {
				t.Fatal("expected comment, got nil")
			}
			if c.ID != "3" {
				t.Errorf("expected newest (id=3), got %s", c.ID)
			}
		},
	},
	{
		name: "ScanForMarker_NoMatch",
		handlers: scenarioHandlers{
			Gitea: func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, `[{"id":1,"body":"nothing here"}]`)
			},
			GitHub: func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, `[{"id":1,"body":"nothing here"}]`)
			},
		},
		assert: func(t *testing.T, adapter Upstream) {
			c, err := adapter.ScanForMarker(context.Background(), &PullRequest{Number: 42}, "<!-- nimblegate-data -->")
			if err != nil {
				t.Fatalf("ScanForMarker: %v", err)
			}
			if c != nil {
				t.Errorf("expected nil, got %+v", c)
			}
		},
	},
	{
		name: "CreateComment_BodyPosted",
		handlers: scenarioHandlers{
			Gitea: func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" {
					http.Error(w, "wrong method", http.StatusMethodNotAllowed)
					return
				}
				var got map[string]string
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil || got["body"] != "hello world" {
					http.Error(w, "bad body", http.StatusBadRequest)
					return
				}
				w.WriteHeader(http.StatusCreated)
				_, _ = io.WriteString(w, `{"id":777,"body":"hello world","html_url":"https://host/you/nimblegate/pulls/42#issuecomment-777"}`)
			},
			GitHub: func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "POST" {
					http.Error(w, "wrong method", http.StatusMethodNotAllowed)
					return
				}
				var got map[string]string
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil || got["body"] != "hello world" {
					http.Error(w, "bad body", http.StatusBadRequest)
					return
				}
				w.WriteHeader(http.StatusCreated)
				_, _ = io.WriteString(w, `{"id":777,"body":"hello world","html_url":"https://host/you/nimblegate/pull/42#issuecomment-777"}`)
			},
		},
		assert: func(t *testing.T, adapter Upstream) {
			c, err := adapter.CreateComment(context.Background(), &PullRequest{Number: 42}, "hello world")
			if err != nil {
				t.Fatalf("CreateComment: %v", err)
			}
			if c == nil {
				t.Fatal("expected comment, got nil")
			}
			if c.ID != "777" {
				t.Errorf("ID = %q, want 777", c.ID)
			}
			if c.Body != "hello world" {
				t.Errorf("Body = %q", c.Body)
			}
		},
	},
	{
		name: "UpdateComment_Success",
		handlers: scenarioHandlers{
			Gitea: func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "PATCH" {
					http.Error(w, "wrong method", http.StatusMethodNotAllowed)
					return
				}
				var got map[string]string
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil || got["body"] != "updated" {
					http.Error(w, "bad body", http.StatusBadRequest)
					return
				}
				_, _ = io.WriteString(w, `{"id":555,"body":"updated"}`)
			},
			GitHub: func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "PATCH" {
					http.Error(w, "wrong method", http.StatusMethodNotAllowed)
					return
				}
				var got map[string]string
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil || got["body"] != "updated" {
					http.Error(w, "bad body", http.StatusBadRequest)
					return
				}
				_, _ = io.WriteString(w, `{"id":555,"body":"updated"}`)
			},
		},
		assert: func(t *testing.T, adapter Upstream) {
			if err := adapter.UpdateComment(context.Background(), &Comment{ID: "555"}, "updated"); err != nil {
				t.Fatalf("UpdateComment: %v", err)
			}
		},
	},
	{
		name: "UpdateComment_404_IsTransient",
		handlers: scenarioHandlers{
			Gitea: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, `{"message":"not found"}`, http.StatusNotFound)
			},
			GitHub: func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, `{"message":"Not Found"}`, http.StatusNotFound)
			},
		},
		assert: func(t *testing.T, adapter Upstream) {
			err := adapter.UpdateComment(context.Background(), &Comment{ID: "999"}, "x")
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !errors.Is(err, ErrTransient) {
				t.Errorf("expected ErrTransient (sticky deleted mid-flight is retryable), got %v", err)
			}
		},
	},
	{
		name: "Errors_5xxAnd429Transient_4xxPermanent",
		// This row drives one assert that runs an inner table - each
		// adapter sees the same status-code → sentinel mapping. We
		// rebuild a fresh server per status inside the assert via the
		// runner's helper, so the handlers here are unused dummies.
		handlers: scenarioHandlers{
			Gitea:  func(w http.ResponseWriter, r *http.Request) {},
			GitHub: func(w http.ResponseWriter, r *http.Request) {},
		},
		assert: nil, // handled specially by runUpstreamSuite - see below.
	},
}

// runUpstreamSuite drives every shared scenario against a single adapter.
// host is "gitea" or "github" - selects which per-scenario HandlerFunc to
// use. newAdapter wires a fresh adapter onto a fresh httptest.Server.
func runUpstreamSuite(t *testing.T, host string, newAdapter adapterFactory) {
	t.Helper()
	for _, sc := range sharedScenarios {
		sc := sc // capture
		t.Run(sc.name, func(t *testing.T) {
			if sc.name == "Errors_5xxAnd429Transient_4xxPermanent" {
				runErrorClassificationTable(t, newAdapter)
				return
			}
			var handler http.HandlerFunc
			switch host {
			case "gitea":
				handler = sc.handlers.Gitea
			case "github":
				handler = sc.handlers.GitHub
			default:
				t.Fatalf("unknown host: %s", host)
			}
			srv := httptest.NewServer(handler)
			defer srv.Close()
			adapter := newAdapter(t, srv)
			sc.assert(t, adapter)
		})
	}
}

// runErrorClassificationTable is the special-cased table for the error-
// classification scenario. The mapping is identical across hosts:
//   - 500, 502, 429 → ErrTransient (retryable)
//   - 401, 403, 422 → ErrPermanent (deadletter)
//
// (GitHub also has the 403-with-X-RateLimit-Remaining: 0 carve-out, but
// that's a host-specific quirk and lives in github_test.go, not in the
// shared contract.)
func runErrorClassificationTable(t *testing.T, newAdapter adapterFactory) {
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
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "boom", tc.status)
			}))
			defer srv.Close()
			adapter := newAdapter(t, srv)
			_, err := adapter.FindPRForRef(context.Background(), "you/nimblegate", "refs/heads/main")
			if err == nil {
				t.Fatalf("status %d: expected error, got nil", tc.status)
			}
			if !errors.Is(err, tc.want) {
				t.Errorf("status %d: want %v, got %v", tc.status, tc.want, err)
			}
		})
	}
}

// TestUpstreamSuite_Gitea runs the shared conformance suite against the
// Gitea adapter. New scenarios added to sharedScenarios automatically
// run here.
func TestUpstreamSuite_Gitea(t *testing.T) {
	runUpstreamSuite(t, "gitea", func(t *testing.T, srv *httptest.Server) Upstream {
		return NewGiteaAdapter(srv.URL+"/you/nimblegate", "fake-token")
	})
}

// TestUpstreamSuite_GitHub runs the shared conformance suite against
// the GitHub adapter. We override apiBase to point at the test server
// so requests land on /repos/... directly (mirroring github_test.go's
// newGitHubTest helper). New scenarios added to sharedScenarios
// automatically run here.
func TestUpstreamSuite_GitHub(t *testing.T) {
	runUpstreamSuite(t, "github", func(t *testing.T, srv *httptest.Server) Upstream {
		a := NewGitHubAdapter("https://github.com/you/nimblegate", "fake-token")
		a.apiBase = srv.URL
		return a
	})
}
