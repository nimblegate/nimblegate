// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGatewayBenchmarkScore(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "a-go")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	lines := `{"time":"2026-05-26T00:00:00Z","repo":"a-go","refs":["refs/heads/main"],"accept":false,"findings":[{"id":"security/no-hardcoded-credentials","severity":"BLOCK","message":"x:1"}]}
{"time":"2026-05-26T00:01:00Z","repo":"a-go","refs":["refs/heads/main"],"accept":true}
`
	if err := os.WriteFile(filepath.Join(dir, "audit.log"), []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(root, "benchmark.toml")
	os.WriteFile(cfgPath, []byte(`
[scored]
frames = ["security/no-hardcoded-credentials"]
[[run]]
repo = "a-go"
agent = "A"
task = "t1"
stack = "go"
rep = 1
`), 0o644)

	out := captureStdout(t, func() int {
		return gatewayBenchmark([]string{"score", "--config", cfgPath, "--policy-root", root, "--json"})
	})
	if !strings.Contains(out, `"agent": "A"`) || !strings.Contains(out, `"stack": "go"`) {
		t.Errorf("json missing cell: %s", out)
	}

	tbl := captureStdout(t, func() int {
		return gatewayBenchmark([]string{"score", "--config", cfgPath, "--policy-root", root})
	})
	if !strings.Contains(tbl, "go") || !strings.Contains(tbl, "A") {
		t.Errorf("table missing content: %s", tbl)
	}

	if code := gatewayBenchmark([]string{}); code != 2 {
		t.Errorf("no args → exit 2, got %d", code)
	}

	badCfg := filepath.Join(root, "bad.toml")
	os.WriteFile(badCfg, []byte(`
[scored]
frames = ["security/this-frame-does-not-exist"]
[[run]]
repo = "a-go"
agent = "A"
task = "t1"
stack = "go"
rep = 1
`), 0o644)
	if code := gatewayBenchmark([]string{"score", "--config", badCfg, "--policy-root", root}); code != 1 {
		t.Errorf("unknown scored frame → exit 1, got %d", code)
	}
}
