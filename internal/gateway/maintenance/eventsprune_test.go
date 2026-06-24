// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package maintenance

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func evLine(t *testing.T, ts time.Time) string {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"ts": ts, "event": "x", "ok": true})
	return string(b)
}

func TestRunEventsPrune_TimePrunesAndKeepsUnparseable(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 6, 22, 12, 0, 0, 0, time.UTC)
	old := now.Add(-40 * 24 * time.Hour)
	fresh := now.Add(-1 * time.Hour)
	data := evLine(t, old) + "\n" + evLine(t, fresh) + "\n" + "{bad\n"
	if err := os.WriteFile(filepath.Join(root, "_events.jsonl"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}

	res := runEventsPrune(func() time.Time { return now }, root, 30*24*time.Hour)
	if res.Err != nil {
		t.Fatalf("err: %v", res.Err)
	}
	if res.Pruned != 1 || res.Kept != 1 || res.KeptUnparseable != 1 {
		t.Fatalf("want pruned=1 kept=1 unparseable=1, got pruned=%d kept=%d unparseable=%d",
			res.Pruned, res.Kept, res.KeptUnparseable)
	}
}

func TestRunEventsPrune_MissingFileNoOp(t *testing.T) {
	res := runEventsPrune(func() time.Time { return time.Now() }, t.TempDir(), 30*24*time.Hour)
	if res.Err != nil || res.Scanned != 0 {
		t.Fatalf("want clean no-op, got %+v", res)
	}
}
