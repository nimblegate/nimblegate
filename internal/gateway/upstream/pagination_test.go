// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package upstream

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// pagedComments serves n comments, size per page, by the page query param.
// Comment i has id i; ids in marked carry the marker.
func pagedComments(t *testing.T, sizeParam string, size, n int, marked ...int) *httptest.Server {
	t.Helper()
	isMarked := map[int]bool{}
	for _, m := range marked {
		isMarked[m] = true
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if got, _ := strconv.Atoi(r.URL.Query().Get(sizeParam)); got != size {
			t.Errorf("%s = %q, want %d", sizeParam, r.URL.Query().Get(sizeParam), size)
		}
		var items []string
		for i := (page-1)*size + 1; i <= page*size && i <= n; i++ {
			body := "plain"
			if isMarked[i] {
				body = "<!-- nimblegate-data: x -->"
			}
			items = append(items, fmt.Sprintf(`{"id":%d,"body":%q}`, i, body))
		}
		_, _ = w.Write([]byte("[" + strings.Join(items, ",") + "]"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestGiteaScanForMarker_ReadsAllPagesNewestMatch(t *testing.T) {
	srv := pagedComments(t, "limit", giteaPageLimit, 120, 10, 110)
	a := NewGiteaAdapter(srv.URL+"/you/repo", "tok")
	c, err := a.ScanForMarker(context.Background(), &PullRequest{Number: 1}, "<!-- nimblegate-data:")
	if err != nil {
		t.Fatal(err)
	}
	if c == nil || c.ID != "110" {
		t.Fatalf("want the newest marker comment (id 110, page 3), got %+v", c)
	}
}

func TestGitHubScanForMarker_ReadsAllPagesNewestMatch(t *testing.T) {
	srv := pagedComments(t, "per_page", 100, 250, 5, 240)
	a := NewGitHubAdapter(srv.URL+"/you/repo", "tok")
	a.apiBase = srv.URL
	c, err := a.ScanForMarker(context.Background(), &PullRequest{Number: 1}, "<!-- nimblegate-data:")
	if err != nil {
		t.Fatal(err)
	}
	if c == nil || c.ID != "240" {
		t.Fatalf("want the newest marker comment (id 240, page 3), got %+v", c)
	}
}

func TestGiteaFindPRForRef_SecondPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		var items []string
		for i := 1; i <= giteaPageLimit; i++ {
			n := (page-1)*giteaPageLimit + i
			ref := fmt.Sprintf("other-%d", n)
			if n == 60 {
				ref = "feat-x"
			}
			if n > 70 {
				break
			}
			items = append(items, fmt.Sprintf(`{"number":%d,"head":{"ref":%q}}`, n, ref))
		}
		_, _ = w.Write([]byte("[" + strings.Join(items, ",") + "]"))
	}))
	defer srv.Close()
	a := NewGiteaAdapter(srv.URL+"/you/repo", "tok")
	pr, err := a.FindPRForRef(context.Background(), "repo", "refs/heads/feat-x")
	if err != nil {
		t.Fatal(err)
	}
	if pr == nil || pr.Number != 60 {
		t.Fatalf("want PR 60 from page 2, got %+v", pr)
	}
}
