// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package upstream

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// githubUserAgent is sent on every request. GitHub REJECTS requests
// without a User-Agent header, so this is non-optional.
const githubUserAgent = "nimblegate/v0.1.0"

// GitHubAdapter implements Upstream against the GitHub REST API v3.
//
// Constructed with the full repo URL (e.g. https://github.com/you/nimblegate
// or https://ghe.example.com/you/nimblegate) plus a personal access token.
// The adapter derives the API base (https://api.github.com for github.com,
// https://<host>/api/v3 for Enterprise) and owner/repo path. The token is
// sent as `Authorization: Bearer <pat>` and is never included in error
// messages.
type GitHubAdapter struct {
	baseURL   string // original full repo URL, kept for diagnostics
	apiBase   string // e.g. "https://api.github.com" or "https://ghe.example.com/api/v3"
	ownerRepo string // e.g. "you/nimblegate"
	token     string
	client    *http.Client
}

// NewGitHubAdapter builds an adapter for the given repo URL + token.
// baseURL is the FULL repo URL (https://github.com/owner/repo or
// https://<ghe-host>/owner/repo); the adapter derives the API base + owner
// /repo path from it. If the URL is malformed the adapter still
// constructs - calls will fail at request time with a parse error wrapped
// in ErrPermanent.
func NewGitHubAdapter(baseURL, token string) *GitHubAdapter {
	apiBase, ownerRepo := splitGitHubRepoURL(baseURL)
	return &GitHubAdapter{
		baseURL:   baseURL,
		apiBase:   apiBase,
		ownerRepo: ownerRepo,
		token:     token,
		client:    &http.Client{Timeout: 30 * time.Second},
	}
}

// splitGitHubRepoURL turns a repo URL into (apiBase, ownerRepo). For
// github.com the API base is the well-known https://api.github.com host;
// for any other host we assume GitHub Enterprise Server, whose API base
// is https://<host>/api/v3 per GitHub's documentation. For URLs with
// extra path segments beyond owner/repo, only the first two are taken.
func splitGitHubRepoURL(repoURL string) (apiBase, ownerRepo string) {
	repoURL = strings.TrimSuffix(repoURL, "/")
	u, err := url.Parse(repoURL)
	if err != nil || u.Host == "" {
		return "", ""
	}
	path := strings.Trim(u.Path, "/")
	parts := strings.Split(path, "/")
	var repo string
	if len(parts) >= 2 {
		// Strip the ".git" clone-URL suffix; the API path uses "owner/repo".
		repo = parts[0] + "/" + strings.TrimSuffix(parts[1], ".git")
	}
	host := strings.ToLower(u.Host)
	if host == "github.com" || host == "www.github.com" {
		return "https://api.github.com", repo
	}
	return u.Scheme + "://" + u.Host + "/api/v3", repo
}

func (a *GitHubAdapter) Name() string { return "github" }

// FindPRForRef uses GitHub's server-side head-ref filter
// (`?head=owner:branch`) so the response contains at most one PR for the
// given branch - no client-side scan needed. ref may be a full ref or a
// bare branch name; we normalize by stripping refs/heads/.
func (a *GitHubAdapter) FindPRForRef(ctx context.Context, _, ref string) (*PullRequest, error) {
	branch := strings.TrimPrefix(ref, "refs/heads/")
	owner := strings.SplitN(a.ownerRepo, "/", 2)[0]

	endpoint := fmt.Sprintf(
		"%s/repos/%s/pulls?state=open&head=%s:%s",
		a.apiBase, a.ownerRepo, url.QueryEscape(owner), url.QueryEscape(branch),
	)
	body, err := a.do(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	var prs []struct {
		Number  int    `json:"number"`
		URL     string `json:"url"`
		HTMLURL string `json:"html_url"`
		Head    struct {
			Ref string `json:"ref"`
		} `json:"head"`
		CreatedAt time.Time `json:"created_at"`
	}
	if err := json.Unmarshal(body, &prs); err != nil {
		return nil, fmt.Errorf("%w: decode PR list: %v", ErrPermanent, err)
	}

	if len(prs) == 0 {
		return nil, nil
	}
	p := prs[0]
	u := p.HTMLURL
	if u == "" {
		u = p.URL
	}
	return &PullRequest{
		Number:   p.Number,
		URL:      u,
		Ref:      ref,
		OpenedAt: p.CreatedAt,
	}, nil
}

// ReadPRPeople fetches the PR resource ONCE - GitHub returns both
// assignees and requested_reviewers in the same payload, so unlike the
// Gitea adapter there is no separate /requested_reviewers call. Empty
// slices mean "no humans assigned" - a valid state, not an error.
func (a *GitHubAdapter) ReadPRPeople(ctx context.Context, pr *PullRequest) (PRPeople, error) {
	endpoint := fmt.Sprintf("%s/repos/%s/pulls/%d", a.apiBase, a.ownerRepo, pr.Number)
	body, err := a.do(ctx, "GET", endpoint, nil)
	if err != nil {
		return PRPeople{}, err
	}
	var prResp struct {
		Assignees []struct {
			Login string `json:"login"`
		} `json:"assignees"`
		RequestedReviewers []struct {
			Login string `json:"login"`
		} `json:"requested_reviewers"`
	}
	if err := json.Unmarshal(body, &prResp); err != nil {
		return PRPeople{}, fmt.Errorf("%w: decode PR: %v", ErrPermanent, err)
	}

	people := PRPeople{
		Assignees: make([]string, 0, len(prResp.Assignees)),
		Reviewers: make([]string, 0, len(prResp.RequestedReviewers)),
	}
	for _, x := range prResp.Assignees {
		people.Assignees = append(people.Assignees, x.Login)
	}
	for _, x := range prResp.RequestedReviewers {
		people.Reviewers = append(people.Reviewers, x.Login)
	}
	return people, nil
}

// FindStickyComment fetches a single issue comment by ID. A 404 means
// the sticky was deleted out-of-band - return (nil, nil) so the
// orchestrator falls back to ScanForMarker.
func (a *GitHubAdapter) FindStickyComment(ctx context.Context, _ *PullRequest, commentID string) (*Comment, error) {
	endpoint := fmt.Sprintf("%s/repos/%s/issues/comments/%s", a.apiBase, a.ownerRepo, commentID)
	body, err := a.do(ctx, "GET", endpoint, nil)
	if err != nil {
		if isStatus(err, http.StatusNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return decodeGitHubComment(body)
}

// ScanForMarker lists the PR's issue comments and returns the most
// recent one whose body contains marker. v0.1 reads a single page
// (per_page=100); pagination is parked for v0.2. Returns (nil, nil) on
// no match - the orchestrator treats that as "no sticky yet, create one".
func (a *GitHubAdapter) ScanForMarker(ctx context.Context, pr *PullRequest, marker string) (*Comment, error) {
	endpoint := fmt.Sprintf("%s/repos/%s/issues/%d/comments?per_page=100", a.apiBase, a.ownerRepo, pr.Number)
	body, err := a.do(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	var raw []json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("%w: decode comments: %v", ErrPermanent, err)
	}
	// Walk newest-first so the first body containing the marker wins.
	for i := len(raw) - 1; i >= 0; i-- {
		c, err := decodeGitHubComment(raw[i])
		if err != nil {
			return nil, err
		}
		if strings.Contains(c.Body, marker) {
			return c, nil
		}
	}
	return nil, nil
}

// CreateComment POSTs a new comment to the PR's issue comments endpoint
// and returns the created Comment with its server-assigned ID + URL.
func (a *GitHubAdapter) CreateComment(ctx context.Context, pr *PullRequest, body string) (*Comment, error) {
	endpoint := fmt.Sprintf("%s/repos/%s/issues/%d/comments", a.apiBase, a.ownerRepo, pr.Number)
	payload, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		return nil, fmt.Errorf("%w: encode body: %v", ErrPermanent, err)
	}
	respBody, err := a.do(ctx, "POST", endpoint, payload)
	if err != nil {
		return nil, err
	}
	return decodeGitHubComment(respBody)
}

// UpdateComment PATCHes the comment body. A 404 means the comment was
// deleted between the orchestrator picking the sticky + this call - that
// is retryable (the next attempt will fall back to ScanForMarker or
// create a new sticky), so wrap as ErrTransient.
func (a *GitHubAdapter) UpdateComment(ctx context.Context, c *Comment, body string) error {
	endpoint := fmt.Sprintf("%s/repos/%s/issues/comments/%s", a.apiBase, a.ownerRepo, c.ID)
	payload, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		return fmt.Errorf("%w: encode body: %v", ErrPermanent, err)
	}
	if _, err := a.do(ctx, "PATCH", endpoint, payload); err != nil {
		if isStatus(err, http.StatusNotFound) {
			return fmt.Errorf("%w: comment %s deleted mid-flight", ErrTransient, c.ID)
		}
		return err
	}
	return nil
}

// do issues an authenticated HTTP request + reads the body. Non-2xx
// status codes are mapped via classifyGitHubHTTP, which inspects rate-
// limit headers so a 403 with X-RateLimit-Remaining:0 (GitHub's primary
// rate limit) is treated as transient. Network errors are ErrTransient.
// The token is never included in error messages.
func (a *GitHubAdapter) do(ctx context.Context, method, endpoint string, body []byte) ([]byte, error) {
	if a.ownerRepo == "" {
		return nil, fmt.Errorf("%w: invalid repo URL (no owner/repo)", ErrPermanent)
	}
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, fmt.Errorf("%w: build request: %v", ErrPermanent, err)
	}
	req.Header.Set("Authorization", "Bearer "+a.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", githubUserAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTransient, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%w: read response: %v", ErrTransient, err)
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return respBody, nil
	}
	return nil, classifyGitHubHTTP(resp.StatusCode, resp.Header)
}

// classifyGitHubHTTP maps a non-2xx status to the right sentinel,
// special-casing GitHub's primary rate limit: a 403 with
// X-RateLimit-Remaining: 0 is transient (the quota will reset and the
// next retry will succeed), distinct from a 403 for missing scopes or
// bad auth which is permanent.
func classifyGitHubHTTP(status int, headers http.Header) error {
	if status >= 500 || status == http.StatusTooManyRequests {
		return fmt.Errorf("%w: HTTP %d", ErrTransient, status)
	}
	if status == http.StatusForbidden && headers.Get("X-RateLimit-Remaining") == "0" {
		return fmt.Errorf("%w: HTTP %d (rate-limited)", ErrTransient, status)
	}
	return fmt.Errorf("%w: HTTP %d", ErrPermanent, status)
}

// decodeGitHubComment turns GitHub's issue-comment JSON into our Comment
// type. GitHub returns id as an integer; we stringify it for the
// upstream-agnostic Comment.ID contract.
func decodeGitHubComment(body []byte) (*Comment, error) {
	var raw struct {
		ID        int64     `json:"id"`
		Body      string    `json:"body"`
		HTMLURL   string    `json:"html_url"`
		URL       string    `json:"url"`
		CreatedAt time.Time `json:"created_at"`
		UpdatedAt time.Time `json:"updated_at"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("%w: decode comment: %v", ErrPermanent, err)
	}
	u := raw.HTMLURL
	if u == "" {
		u = raw.URL
	}
	return &Comment{
		ID:        fmt.Sprintf("%d", raw.ID),
		URL:       u,
		Body:      raw.Body,
		CreatedAt: raw.CreatedAt,
		UpdatedAt: raw.UpdatedAt,
	}, nil
}
