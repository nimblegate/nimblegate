// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nimblegate/internal/frames"
)

func TestClampToInt(t *testing.T) {
	if got := clampToInt(-5); got != 0 {
		t.Errorf("clampToInt(-5)=%d want 0", got)
	}
	if got := clampToInt(10); got != 10 {
		t.Errorf("clampToInt(10)=%d want 10", got)
	}
	if got := clampToInt(1 << 40); got != int(^uint32(0)>>1) {
		t.Errorf("clampToInt(huge)=%d want MaxInt32", got)
	}
}

func TestAudit_AppendsJSONLine(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "audit.log")
	a, err := OpenAudit(logPath)
	if err != nil {
		t.Fatalf("OpenAudit: %v", err)
	}
	defer a.Close()

	r := CheckResult{
		FrameID:   "git-safety/folder-branch-lock",
		Category:  frames.CategoryGitSafety,
		Outcome:   OutcomeBlock,
		Reason:    "current folder 'infra/' expects branch 'infra' but pushing 'landing'",
		Override:  false,
		Timestamp: time.Date(2026, 5, 14, 18, 23, 14, 0, time.UTC),
	}
	if err := a.Write(CheckContext{Trigger: TriggerGitWrap, Command: "git push origin landing"}, r); err != nil {
		t.Fatal(err)
	}
	partPath := a.partPath
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	// Writes go to the per-process part file under audit.parts/, not the
	// logical audit.log. Read from there to verify content.
	data, err := os.ReadFile(partPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"frame":"git-safety/folder-branch-lock"`) {
		t.Errorf("log missing frame id; got: %s", data)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &got); err != nil {
		t.Fatalf("log is not valid JSON line: %v", err)
	}
	if got["trigger"] != "git-wrap" {
		t.Errorf("trigger field = %v, want git-wrap", got["trigger"])
	}
	if got["result"] != "BLOCK" {
		t.Errorf("result field = %v, want BLOCK", got["result"])
	}
}
