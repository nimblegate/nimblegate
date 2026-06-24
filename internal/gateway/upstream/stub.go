// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package upstream

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Stub is an in-memory implementation of Upstream for orchestrator tests.
// Production code never uses it - gitea + github adapters do real HTTP.
type Stub struct {
	mu       sync.Mutex
	prs      map[string]map[string]*PullRequest // repo → ref → PR
	people   map[int]PRPeople                   // PR number → people
	comments map[int][]*Comment                 // PR number → comments
	nextID   int
}

func NewStub() *Stub {
	return &Stub{
		prs:      map[string]map[string]*PullRequest{},
		people:   map[int]PRPeople{},
		comments: map[int][]*Comment{},
	}
}

func (s *Stub) Name() string { return "stub" }

// AddPR seeds the stub with a PR on (repo, ref).
func (s *Stub) AddPR(repo, ref string, pr *PullRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.prs[repo] == nil {
		s.prs[repo] = map[string]*PullRequest{}
	}
	pr.Ref = ref
	s.prs[repo][ref] = pr
}

// SetPeople seeds assignees/reviewers for a PR.
func (s *Stub) SetPeople(prNumber int, people PRPeople) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.people[prNumber] = people
}

// Comments returns a snapshot of the comments seeded/created on prNumber.
// Test-only accessor - orchestrator tests assert on Create/Update outcomes.
func (s *Stub) Comments(prNumber int) []*Comment {
	s.mu.Lock()
	defer s.mu.Unlock()
	src := s.comments[prNumber]
	out := make([]*Comment, len(src))
	copy(out, src)
	return out
}

// AddComment seeds prNumber with c (no API call). Useful for the
// "stale sticky ID falls back to ScanForMarker" test case where the
// existing comment carries the hidden nimblegate-data marker.
func (s *Stub) AddComment(prNumber int, c *Comment) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.comments[prNumber] = append(s.comments[prNumber], c)
}

func (s *Stub) FindPRForRef(ctx context.Context, repo, ref string) (*PullRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.prs[repo]; ok {
		return m[ref], nil // nil if no PR on ref (no-PR case)
	}
	return nil, nil
}

func (s *Stub) ReadPRPeople(ctx context.Context, pr *PullRequest) (PRPeople, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.people[pr.Number], nil
}

func (s *Stub) FindStickyComment(ctx context.Context, pr *PullRequest, id string) (*Comment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.comments[pr.Number] {
		if c.ID == id {
			return c, nil
		}
	}
	return nil, nil
}

func (s *Stub) ScanForMarker(ctx context.Context, pr *PullRequest, marker string) (*Comment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cs := s.comments[pr.Number]
	for i := len(cs) - 1; i >= 0; i-- { // newest first
		if strings.Contains(cs[i].Body, marker) {
			return cs[i], nil
		}
	}
	return nil, nil
}

func (s *Stub) CreateComment(ctx context.Context, pr *PullRequest, body string) (*Comment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextID++
	c := &Comment{ID: fmt.Sprintf("comment_%d", s.nextID), Body: body, CreatedAt: time.Now().UTC()}
	s.comments[pr.Number] = append(s.comments[pr.Number], c)
	return c, nil
}

func (s *Stub) UpdateComment(ctx context.Context, c *Comment, body string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, list := range s.comments {
		for _, existing := range list {
			if existing.ID == c.ID {
				existing.Body = body
				existing.UpdatedAt = time.Now().UTC()
				return nil
			}
		}
	}
	return fmt.Errorf("%w: comment %s not found", ErrPermanent, c.ID)
}
