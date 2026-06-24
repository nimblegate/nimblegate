// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package analytics

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// seedAgent inserts decisions across two repos: "hot" 4 decisions/3 rejects,
// "calm" 10 decisions/1 reject, "tiny" 1 decision/1 reject (below min cutoff).
func seedAgent(t *testing.T) *DB {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "analytics.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	now := time.Now().Unix()
	ins := func(repo string, accept int, sev, dedup string) int64 {
		res, err := d.sql.Exec(
			`INSERT INTO decisions(ts,repo,accept,refs,max_severity,dedup) VALUES(?,?,?,?,?,?)`,
			now, repo, accept, `["refs/heads/main"]`, sev, dedup)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	var lastRej int64
	for i := 0; i < 3; i++ {
		id := ins("hot", 0, "BLOCK", "hot-rej-"+string(rune('a'+i)))
		if _, err := d.sql.Exec(
			`INSERT INTO findings(decision_id,frame_id,severity,message,fingerprint) VALUES(?,?,?,?,?)`,
			id, "security/no-private-keys-in-repo", "BLOCK", "key found", "fp1"); err != nil {
			t.Fatal(err)
		}
		lastRej = id
	}
	for i, f := range []struct{ frame, sev string }{
		{"style/info-a", "INFO"}, {"style/info-b", "INFO"}, {"git-safety/warn-a", "WARN"}, {"app/error-a", "ERROR"},
	} {
		if _, err := d.sql.Exec(
			`INSERT INTO findings(decision_id,frame_id,severity,message,fingerprint) VALUES(?,?,?,?,?)`,
			lastRej, f.frame, f.sev, "m", "fpx"+string(rune('a'+i))); err != nil {
			t.Fatal(err)
		}
	}
	ins("hot", 1, "", "hot-ok")
	for i := 0; i < 9; i++ {
		ins("calm", 1, "", "calm-ok-"+string(rune('a'+i)))
	}
	ins("calm", 0, "ERROR", "calm-rej")
	ins("tiny", 0, "BLOCK", "tiny-rej")
	return d
}

func TestBounceRate(t *testing.T) {
	d := seedAgent(t)
	out, err := BounceRate(d, StatsQuery{}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("tiny (1 decision) must be excluded by minDecisions=2: %+v", out)
	}
	if out[0].Repo != "hot" || out[0].Rejects != 3 || out[0].Decisions != 4 {
		t.Errorf("hot should rank first: %+v", out[0])
	}
	if out[0].Rate <= out[1].Rate {
		t.Errorf("not sorted by rate desc: %+v", out)
	}
	if out[0].Rate != 0.75 {
		t.Errorf("hot rate: want 0.75 got %v", out[0].Rate)
	}
}

func TestRecentDecisions(t *testing.T) {
	d := seedAgent(t)
	rej, err := RecentDecisions(d, StatsQuery{Repo: "hot"}, "rejected", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rej) != 3 {
		t.Fatalf("want 3 rejected hot decisions: %+v", rej)
	}
	if rej[0].Accept || rej[0].Repo != "hot" || rej[0].MaxSeverity != "BLOCK" {
		t.Errorf("summary fields wrong: %+v", rej[0])
	}
	if len(rej[0].TopFindings) != 3 {
		t.Fatalf("cap at 3 findings: %+v", rej[0].TopFindings)
	}
	if !strings.Contains(rej[0].TopFindings[0], "(BLOCK)") || !strings.Contains(rej[0].TopFindings[1], "(ERROR)") || !strings.Contains(rej[0].TopFindings[2], "(WARN)") {
		t.Errorf("findings must be severity-ordered BLOCK>ERROR>WARN: %+v", rej[0].TopFindings)
	}
	if len(rej[1].TopFindings) != 1 || !strings.Contains(rej[1].TopFindings[0], "security/no-private-keys-in-repo (BLOCK)") {
		t.Errorf("single-finding decision wrong: %+v", rej[1].TopFindings)
	}
	if !strings.Contains(rej[0].Refs, "refs/heads/main") {
		t.Errorf("refs missing: %q", rej[0].Refs)
	}
	all, err := RecentDecisions(d, StatsQuery{}, "", 2)
	if err != nil || len(all) != 2 {
		t.Fatalf("limit not applied: %v %v", all, err)
	}
	if _, err := RecentDecisions(d, StatsQuery{}, "accepted", 999); err != nil {
		t.Fatal(err) // limit must clamp internally, not error
	}
}

func TestRepos(t *testing.T) {
	d := seedAgent(t)
	repos, err := Repos(d)
	if err != nil || strings.Join(repos, ",") != "calm,hot,tiny" {
		t.Fatalf("got %v err %v", repos, err)
	}
}
