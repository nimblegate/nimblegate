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

// GiteaAdapter implements Upstream against the Gitea HTTP API v1.
//
// Constructed with the full repo URL (e.g. https://gitea.example.com/you/nimblegate)
// + a personal access token. The adapter splits the URL into API base
// (https://gitea.example.com) and owner/repo path (you/nimblegate) for endpoint
// composition. The token is sent as `Authorization: token <pat>` per Gitea
// convention and is never included in error messages.
type GiteaAdapter struct {
	apiBase   string // e.g. "https://gitea.example.com"
	ownerRepo string // e.g. "you/nimblegate"
	token     string
	client    *http.Client
}

// NewGiteaAdapter builds an adapter for the given repo URL + token.
// baseURL is the FULL repo URL (https://host/owner/repo); the adapter
// derives the API base + owner/repo path from it. If the URL is malformed
// the adapter still constructs - calls will fail at request time with a
// parse error wrapped in ErrPermanent.
func NewGiteaAdapter(baseURL, token string) *GiteaAdapter {
	apiBase, ownerRepo := splitRepoURL(baseURL)
	return &GiteaAdapter{
		apiBase:   apiBase,
		ownerRepo: ownerRepo,
		token:     token,
		client:    &http.Client{Timeout: 30 * time.Second},
	}
}

// splitRepoURL turns "https://gitea.example.com/you/nimblegate" into
// ("https://gitea.example.com", "you/nimblegate"). For URLs with extra path
// segments beyond owner/repo, only the first two segments are taken as
// owner/repo.
func splitRepoURL(repoURL string) (apiBase, ownerRepo string) {
	repoURL = strings.TrimSuffix(repoURL, "/")
	u, err := url.Parse(repoURL)
	if err != nil || u.Host == "" {
		return "", ""
	}
	path := strings.Trim(u.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return u.Scheme + "://" + u.Host, ""
	}
	// Strip the ".git" clone-URL suffix - the registered upstream URL is the
	// clone URL ("…/owner/repo.git"), but the Gitea API path is "owner/repo"
	// (with ".git" it 404s). This is why PR-comment delivery failed for every
	// real upstream.
	return u.Scheme + "://" + u.Host, parts[0] + "/" + strings.TrimSuffix(parts[1], ".git")
}

func (a *GiteaAdapter) Name() string { return "gitea" }

// FindPRForRef lists open PRs on the repo and scans client-side for one
// whose head ref matches the given ref. Gitea's pulls endpoint has no
// head-ref filter parameter, so list-then-scan is the only path. ref may
// be either a full ref ("refs/heads/main") or a bare branch name - Gitea
// returns bare branch names so we normalize before comparing.
func (a *GiteaAdapter) FindPRForRef(ctx context.Context, _, ref string) (*PullRequest, error) {
	branch := strings.TrimPrefix(ref, "refs/heads/")

	endpoint := fmt.Sprintf("%s/api/v1/repos/%s/pulls?state=open", a.apiBase, a.ownerRepo)
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

	for _, p := range prs {
		if p.Head.Ref == branch {
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
	}
	return nil, nil
}

// ReadPRPeople fetches the PR (for assignees) plus the requested_reviewers
// endpoint, returning logins with no @ prefix. Empty slices mean "no
// humans assigned" - a valid state, not an error.
func (a *GiteaAdapter) ReadPRPeople(ctx context.Context, pr *PullRequest) (PRPeople, error) {
	prEndpoint := fmt.Sprintf("%s/api/v1/repos/%s/pulls/%d", a.apiBase, a.ownerRepo, pr.Number)
	prBody, err := a.do(ctx, "GET", prEndpoint, nil)
	if err != nil {
		return PRPeople{}, err
	}
	var prResp struct {
		Assignees []struct {
			Login string `json:"login"`
		} `json:"assignees"`
	}
	if err := json.Unmarshal(prBody, &prResp); err != nil {
		return PRPeople{}, fmt.Errorf("%w: decode PR: %v", ErrPermanent, err)
	}

	people := PRPeople{Assignees: make([]string, 0, len(prResp.Assignees))}
	for _, as := range prResp.Assignees {
		people.Assignees = append(people.Assignees, as.Login)
	}

	// Requested reviewers are best-effort: several Gitea versions don't expose
	// the GitHub-style /requested_reviewers endpoint (it 404s), and reviewers
	// are optional for the @-mention. A failure here must NOT sink the whole
	// PR-comment delivery - proceed with assignees only.
	revEndpoint := fmt.Sprintf("%s/api/v1/repos/%s/pulls/%d/requested_reviewers", a.apiBase, a.ownerRepo, pr.Number)
	if revBody, err := a.do(ctx, "GET", revEndpoint, nil); err == nil {
		var revResp []struct {
			Login string `json:"login"`
		}
		if json.Unmarshal(revBody, &revResp) == nil {
			for _, r := range revResp {
				people.Reviewers = append(people.Reviewers, r.Login)
			}
		}
	}
	return people, nil
}

// FindStickyComment fetches a single comment by ID. A 404 means the
// sticky was deleted out-of-band - return (nil, nil) so the orchestrator
// falls back to ScanForMarker.
func (a *GiteaAdapter) FindStickyComment(ctx context.Context, _ *PullRequest, commentID string) (*Comment, error) {
	endpoint := fmt.Sprintf("%s/api/v1/repos/%s/issues/comments/%s", a.apiBase, a.ownerRepo, commentID)
	body, err := a.do(ctx, "GET", endpoint, nil)
	if err != nil {
		if isStatus(err, http.StatusNotFound) {
			return nil, nil
		}
		return nil, err
	}
	c, err := decodeComment(body)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// ScanForMarker walks the PR's issue comments newest-first and returns
// the most recent one whose body contains marker. Returns (nil, nil) if
// no match - the orchestrator treats that as "no sticky yet, create one".
func (a *GiteaAdapter) ScanForMarker(ctx context.Context, pr *PullRequest, marker string) (*Comment, error) {
	endpoint := fmt.Sprintf("%s/api/v1/repos/%s/issues/%d/comments", a.apiBase, a.ownerRepo, pr.Number)
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
		c, err := decodeComment(raw[i])
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
func (a *GiteaAdapter) CreateComment(ctx context.Context, pr *PullRequest, body string) (*Comment, error) {
	endpoint := fmt.Sprintf("%s/api/v1/repos/%s/issues/%d/comments", a.apiBase, a.ownerRepo, pr.Number)
	payload, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		return nil, fmt.Errorf("%w: encode body: %v", ErrPermanent, err)
	}
	respBody, err := a.do(ctx, "POST", endpoint, payload)
	if err != nil {
		return nil, err
	}
	return decodeComment(respBody)
}

// UpdateComment PATCHes the comment body. A 404 means the comment was
// deleted between the orchestrator picking the sticky + this call - that
// is retryable (the next attempt will fall back to ScanForMarker or
// create a new sticky), so wrap as ErrTransient.
func (a *GiteaAdapter) UpdateComment(ctx context.Context, c *Comment, body string) error {
	endpoint := fmt.Sprintf("%s/api/v1/repos/%s/issues/comments/%s", a.apiBase, a.ownerRepo, c.ID)
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
// status codes are mapped to ErrTransient (5xx, 429) or ErrPermanent
// (other 4xx) via classifyHTTP. Network errors are ErrTransient. The
// token is never included in error messages.
func (a *GiteaAdapter) do(ctx context.Context, method, endpoint string, body []byte) ([]byte, error) {
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
	req.Header.Set("Authorization", "token "+a.token)
	req.Header.Set("Accept", "application/json")
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
	return nil, classifyHTTP(resp.StatusCode)
}

// classifyHTTP maps a non-2xx status code to the right sentinel.
// 5xx + 429 are transient (retry with backoff); 4xx are permanent
// (deadletter). The status code is included so the caller can match it
// via isStatus when a method needs to special-case (e.g. 404).
func classifyHTTP(status int) error {
	if status >= 500 || status == http.StatusTooManyRequests {
		return fmt.Errorf("%w: HTTP %d", ErrTransient, status)
	}
	return fmt.Errorf("%w: HTTP %d", ErrPermanent, status)
}

// isStatus reports whether err carries an "HTTP <code>" classification
// matching status. Used by FindStickyComment + UpdateComment to peel off
// 404 from the generic permanent/transient bucket.
func isStatus(err error, status int) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), fmt.Sprintf("HTTP %d", status))
}

// decodeComment turns Gitea's issue-comment JSON into our Comment type.
// Gitea returns id as an integer; we stringify it for the upstream-
// agnostic Comment.ID contract.
func decodeComment(body []byte) (*Comment, error) {
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
