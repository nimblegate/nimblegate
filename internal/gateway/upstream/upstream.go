// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

// Package upstream defines the host-agnostic contract for posting PR
// comments + reading PR metadata. Adapters in this package (gitea.go,
// github.go, stub.go) implement Upstream. The orchestrator in the
// notification package consumes the interface.
package upstream

import (
	"context"
	"errors"
	"time"
)

// Upstream is the contract every git-host adapter implements per spec §8.2.
// Methods are scoped to "operations the notification rail needs" - NOT a
// general-purpose upstream API client.
type Upstream interface {
	Name() string

	FindPRForRef(ctx context.Context, repo, ref string) (*PullRequest, error)
	ReadPRPeople(ctx context.Context, pr *PullRequest) (PRPeople, error)

	FindStickyComment(ctx context.Context, pr *PullRequest, commentID string) (*Comment, error)
	ScanForMarker(ctx context.Context, pr *PullRequest, marker string) (*Comment, error)

	CreateComment(ctx context.Context, pr *PullRequest, body string) (*Comment, error)
	UpdateComment(ctx context.Context, c *Comment, body string) error
}

// PullRequest is the upstream-agnostic projection of an open PR on a ref.
type PullRequest struct {
	Number   int
	URL      string
	Ref      string
	OpenedAt time.Time
}

// PRPeople is the live assignee/reviewer list. Empty slices are valid
// ("no humans assigned").
type PRPeople struct {
	Assignees []string // login handles, no @ prefix
	Reviewers []string
}

// Comment is the upstream-agnostic projection of a comment.
type Comment struct {
	ID        string
	URL       string
	Body      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ErrPermanent + ErrTransient classify upstream errors so the orchestrator
// can route to retry (transient) vs deadletter (permanent). Adapters wrap
// their underlying errors with one of these so the orchestrator's logic
// stays generic.
var (
	ErrPermanent = errors.New("upstream: permanent error")
	ErrTransient = errors.New("upstream: transient error")
)
