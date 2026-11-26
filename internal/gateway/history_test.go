// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nimblegate/internal/engine"
)

func TestPlanHistoryChunks_VersionsOfOnePathSplit(t *testing.T) {
	blobs := []historyBlob{
		{path: "a.txt", blob: "1", size: 10},
		{path: "a.txt", blob: "2", size: 10},
		{path: "b.txt", blob: "3", size: 10},
		{path: "dir", blob: "4", size: 10},
		{path: "dir/x", blob: "5", size: 10},
	}
	chunks := planHistoryChunks(blobs, 1<<20)
	if len(chunks) != 2 {
		t.Fatalf("want 2 chunks, got %d", len(chunks))
	}
	for _, c := range chunks {
		if c.files["dir"] && c.dirs["dir"] {
			t.Errorf("file and directory 'dir' planned into one chunk: %+v", c.blobs)
		}
	}
	if !chunks[0].files["a.txt"] || !chunks[1].files["a.txt"] {
		t.Errorf("each version of a.txt needs its own chunk")
	}
}

func TestPlanHistoryChunks_SizeCap(t *testing.T) {
	blobs := []historyBlob{{path: "a", size: 60}, {path: "b", size: 60}, {path: "c", size: 200}}
	chunks := planHistoryChunks(blobs, 100)
	if len(chunks) != 3 {
		t.Fatalf("want 3 chunks under a 100-byte cap (an oversized blob gets its own), got %d", len(chunks))
	}
}

func TestParseDiffTree_SkipsTipSymlinksAndUnsafePaths(t *testing.T) {
	z := "\x00"
	out := "c1" + z +
		":000000 100644 0000 b1 A" + z + "keep.txt" + z +
		":000000 100644 0000 b2 A" + z + "same.txt" + z +
		":000000 120000 0000 b3 A" + z + "link" + z +
		":000000 160000 0000 b4 A" + z + "sub" + z +
		":000000 100644 0000 b5 A" + z + "../escape" + z +
		"c2 (from c1)" + z +
		":100644 100644 b1 b1 M" + z + "keep.txt" + z
	got := parseDiffTree([]byte(out), map[string]string{"same.txt": "b2"})
	if len(got) != 1 || got[0].path != "keep.txt" || got[0].commit != "c1" {
		t.Fatalf("want only keep.txt from c1, got %+v", got)
	}
}

func TestMergeByFrame_OneLinePerFrame(t *testing.T) {
	in := []engine.CheckResult{
		{FrameID: "security/a", Outcome: engine.OutcomeWarn, Reason: "first", Hits: []engine.Hit{{File: "x"}}},
		{FrameID: "security/b", Outcome: engine.OutcomeBlock, Reason: "other"},
		{FrameID: "security/a", Outcome: engine.OutcomeBlock, Reason: "second", Hits: []engine.Hit{{File: "y"}}},
	}
	out := mergeByFrame(in)
	if len(out) != 2 || out[0].FrameID != "security/a" || out[0].Outcome != engine.OutcomeBlock || len(out[0].Hits) != 2 {
		t.Fatalf("want security/a merged to BLOCK with 2 hits, got %+v", out)
	}
	if !strings.Contains(out[0].Reason, "+1 more earlier file version") {
		t.Errorf("merged reason should count the rest: %q", out[0].Reason)
	}
}

type slowHistoryChecker struct{ delay time.Duration }

func (s slowHistoryChecker) Check(string) ([]engine.CheckResult, []engine.SuppressionLog, error) {
	return nil, nil, nil
}

func (s slowHistoryChecker) CheckHistory(string) ([]engine.CheckResult, []engine.SuppressionLog, error) {
	time.Sleep(s.delay)
	return nil, nil, nil
}

// A history the budget cannot cover blocks with a push-in-parts message
// instead of relaying unscanned content.
func TestScanHistory_OverBudgetBlocks(t *testing.T) {
	work := t.TempDir()
	git := func(args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", work, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q", "-b", "main")
	for i, body := range []string{"one\n", "two\n"} {
		if err := os.WriteFile(filepath.Join(work, "f.txt"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		git("add", "f.txt")
		git("commit", "-qm", string(rune('a'+i)))
	}
	bare := filepath.Join(t.TempDir(), "r.git")
	if out, err := exec.Command("git", "init", "-q", "--bare", bare).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if out, err := exec.Command("git", "-C", bare, "fetch", "-q", work, "main").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	d := PreReceiveDeps{GitDir: bare, ScanTmpDir: t.TempDir(), Policy: Policy{PolicyDir: t.TempDir()}}
	r := RefUpdate{Name: "refs/heads/main", OldRev: strings.Repeat("0", 40), NewRev: git("rev-parse", "HEAD")}

	res, _, err := scanHistory(d, slowHistoryChecker{delay: 2 * time.Second}, r, time.Now().Add(200*time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 1 || res[0].FrameID != HistoryTooLargeID || res[0].Outcome != engine.OutcomeBlock {
		t.Fatalf("want one %s block, got %+v", HistoryTooLargeID, res)
	}
	if !strings.Contains(res[0].Reason, "push it in parts") {
		t.Errorf("reason should say how to get through: %q", res[0].Reason)
	}

	res, _, err = scanHistory(d, slowHistoryChecker{}, r, time.Time{})
	if err != nil || len(res) != 0 {
		t.Fatalf("a fast clean scan should return nothing, got %+v %v", res, err)
	}
}

func TestGatewayConfig_HistoryWorkers(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "gateway.toml"), []byte("[gateway]\nhistory_workers = 3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadGatewayConfig(root)
	if err != nil || cfg.HistoryWorkers != 3 {
		t.Fatalf("want 3 workers, got %d (%v)", cfg.HistoryWorkers, err)
	}
	if err := os.WriteFile(filepath.Join(root, "gateway.toml"), []byte("[gateway]\nhistory_workers = -1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadGatewayConfig(root); err == nil {
		t.Fatal("negative history_workers must be refused")
	}
}
