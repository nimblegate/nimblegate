// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package benchmark

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/gateway/analytics"
)

func writeAudit(t *testing.T, root, repo string, lines ...string) {
	t.Helper()
	dir := filepath.Join(root, repo)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "audit.log"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func ingest(t *testing.T, root string) *analytics.DB {
	t.Helper()
	db, err := analytics.Open(filepath.Join(root, "analytics.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := analytics.Ingest(db, root); err != nil {
		t.Fatal(err)
	}
	return db
}

const SEC = "security/no-hardcoded-credentials"
const SH = "app-correctness/shellcheck"

func push(ts, accept string, findings string) string {
	return `{"time":"2026-05-26T` + ts + `Z","repo":"R","refs":["refs/heads/main"],"accept":` + accept + `,"findings":[` + findings + `]}`
}

func f(frame, msg string) string {
	return `{"id":"` + frame + `","severity":"BLOCK","message":"` + msg + `"}`
}

func TestScoreMetrics(t *testing.T) {
	root := t.TempDir()
	writeAudit(t, root, "a-go",
		strings.Replace(push("00:00:00", "false", f(SEC, "x.js:1")), `"repo":"R"`, `"repo":"a-go"`, 1),
		strings.Replace(push("00:01:00", "true", ""), `"repo":"R"`, `"repo":"a-go"`, 1),
	)
	writeAudit(t, root, "b-go",
		strings.Replace(push("00:00:00", "false", f(SEC, "x.js:1")), `"repo":"R"`, `"repo":"b-go"`, 1),
		strings.Replace(push("00:01:00", "false", f(SEC, "x.js:1")), `"repo":"R"`, `"repo":"b-go"`, 1),
	)
	db := ingest(t, root)
	cfg := Config{
		Scored: scoredBlock{Frames: []string{SEC}},
		Runs: []Run{
			{Repo: "a-go", Agent: "A", Task: "t1", Stack: "go", Rep: 1},
			{Repo: "b-go", Agent: "B", Task: "t1", Stack: "go", Rep: 1},
		},
	}
	m, err := Score(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	a := cell(t, m, "A", "go")
	if a.Runs != 1 || a.ConvergedRate != 1 || a.Convergence.Mean != 2 {
		t.Errorf("A converge wrong: %+v", a)
	}
	if a.Cleanliness.Mean != 0.5 {
		t.Errorf("A cleanliness want 0.5, got %v", a.Cleanliness.Mean)
	}
	b := cell(t, m, "B", "go")
	if b.ConvergedRate != 0 {
		t.Errorf("B should not converge: %+v", b)
	}
	if b.Recurrence.Mean != 1 {
		t.Errorf("B recurrence want 1, got %v", b.Recurrence.Mean)
	}
	if b.ByFrame[SEC] != 2 {
		t.Errorf("B byFrame[SEC] want 2, got %d", b.ByFrame[SEC])
	}
}

func TestScoreWhitelistAndObserved(t *testing.T) {
	root := t.TempDir()
	writeAudit(t, root, "a-go",
		strings.Replace(push("00:00:00", "false", f(SEC, "test/fixtures.js:1")+","+f(SH, "run.sh:1")), `"repo":"R"`, `"repo":"a-go"`, 1),
		strings.Replace(push("00:01:00", "true", ""), `"repo":"R"`, `"repo":"a-go"`, 1),
	)
	db := ingest(t, root)
	cfg := Config{
		Scored:    scoredBlock{Frames: []string{SEC}},
		Whitelist: []WL{{Frame: SEC, Contains: "test/", Reason: "test fixtures, not live creds"}},
		Runs:      []Run{{Repo: "a-go", Agent: "A", Task: "t1", Stack: "go", Rep: 1}},
	}
	m, err := Score(db, cfg)
	if err != nil {
		t.Fatal(err)
	}
	a := cell(t, m, "A", "go")
	if a.Cleanliness.Mean != 0 || a.Convergence.Mean != 1 {
		t.Errorf("whitelist should zero the scored finding: %+v", a)
	}
	if a.Observed[SH] != 1 {
		t.Errorf("non-scored frame should be observed: %+v", a.Observed)
	}
	if a.ByFrame[SEC] != 0 {
		t.Errorf("whitelisted finding must not be scored: %+v", a.ByFrame)
	}
}

func TestScoreVarianceAcrossReps(t *testing.T) {
	root := t.TempDir()
	writeAudit(t, root, "a-go-1",
		strings.Replace(push("00:00:00", "false", f(SEC, "x:1")), `"repo":"R"`, `"repo":"a-go-1"`, 1),
		strings.Replace(push("00:01:00", "true", ""), `"repo":"R"`, `"repo":"a-go-1"`, 1),
	)
	writeAudit(t, root, "a-go-2",
		strings.Replace(push("00:00:00", "false", f(SEC, "x:1")), `"repo":"R"`, `"repo":"a-go-2"`, 1),
		strings.Replace(push("00:01:00", "false", f(SEC, "x:1")), `"repo":"R"`, `"repo":"a-go-2"`, 1),
		strings.Replace(push("00:02:00", "false", f(SEC, "x:1")), `"repo":"R"`, `"repo":"a-go-2"`, 1),
		strings.Replace(push("00:03:00", "true", ""), `"repo":"R"`, `"repo":"a-go-2"`, 1),
	)
	db := ingest(t, root)
	cfg := Config{
		Scored: scoredBlock{Frames: []string{SEC}},
		Runs: []Run{
			{Repo: "a-go-1", Agent: "A", Task: "t1", Stack: "go", Rep: 1},
			{Repo: "a-go-2", Agent: "A", Task: "t1", Stack: "go", Rep: 2},
		},
	}
	m, _ := Score(db, cfg)
	a := cell(t, m, "A", "go")
	if a.Runs != 2 || a.Convergence.Mean != 3 {
		t.Errorf("mean convergence want 3, got %+v", a)
	}
	if math.Abs(a.Convergence.StdDev-1) > 1e-9 {
		t.Errorf("stddev want 1, got %v", a.Convergence.StdDev)
	}
}

func TestLoadConfigErrors(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "ok.toml")
	os.WriteFile(good, []byte(`
[scored]
frames = ["security/no-hardcoded-credentials"]
[[run]]
repo = "a-go"
agent = "A"
task = "t1"
stack = "go"
rep = 1
`), 0o644)
	cfg, err := LoadConfig(good)
	if err != nil || len(cfg.Scored.Frames) != 1 || len(cfg.Runs) != 1 {
		t.Fatalf("good config: %+v %v", cfg, err)
	}
	if _, err := LoadConfig(filepath.Join(dir, "nope.toml")); err == nil {
		t.Error("missing file should error")
	}
	dup := filepath.Join(dir, "dup.toml")
	os.WriteFile(dup, []byte("[scored]\nframes=[\"x\"]\n[[run]]\nrepo=\"r\"\nagent=\"A\"\ntask=\"t\"\nstack=\"go\"\nrep=1\n[[run]]\nrepo=\"r\"\nagent=\"B\"\ntask=\"t\"\nstack=\"go\"\nrep=1\n"), 0o644)
	if _, err := LoadConfig(dup); err == nil {
		t.Error("duplicate run repo should error")
	}
}

func cell(t *testing.T, m Matrix, agent, stack string) Cell {
	t.Helper()
	for _, c := range m.Cells {
		if c.Agent == agent && c.Stack == stack {
			return c
		}
	}
	t.Fatalf("no cell for %s/%s in %+v", agent, stack, m.Cells)
	return Cell{}
}
