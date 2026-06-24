// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package agentapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func mcpCall(t *testing.T, h http.Handler, auth, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestMCPListsTools(t *testing.T) {
	h := restService(t).MCPHandler()
	code, out := mcpCall(t, h, "Bearer nbg_good",
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	if code != http.StatusOK {
		t.Fatalf("code %d", code)
	}
	b, _ := json.Marshal(out)
	for _, tool := range []string{"gate_stats", "bounce_rate", "top_rules", "recurring_findings", "decisions", "time_saved", "what_changed"} {
		if !strings.Contains(string(b), tool) {
			t.Errorf("tool %s missing: %s", tool, b)
		}
	}
}

func TestMCPCallAndAuth(t *testing.T) {
	h := restService(t).MCPHandler()
	code, out := mcpCall(t, h, "Bearer nbg_good",
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"bounce_rate","arguments":{"days":30,"min_pushes":1}}}`)
	if code != http.StatusOK {
		t.Fatalf("code %d: %v", code, out)
	}
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), "demo") || !strings.Contains(string(b), "33%") {
		t.Errorf("bounce answer missing: %s", b)
	}

	code2, _ := mcpCall(t, h, "Bearer nbg_bad",
		`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`)
	if code2 != http.StatusForbidden {
		t.Errorf("bad token must 403, got %d", code2)
	}
	code3, _ := mcpCall(t, h, "",
		`{"jsonrpc":"2.0","id":4,"method":"tools/list"}`)
	if code3 != http.StatusUnauthorized {
		t.Errorf("missing token must 401, got %d", code3)
	}
}

func mcpResultText(t *testing.T, out map[string]any) string {
	t.Helper()
	result, _ := out["result"].(map[string]any)
	content, _ := result["content"].([]any)
	if len(content) == 0 {
		t.Fatalf("no content in result: %v", out)
	}
	first, _ := content[0].(map[string]any)
	s, _ := first["text"].(string)
	return s
}

func TestMCPFormatJSON(t *testing.T) {
	h := restService(t).MCPHandler()
	// Default (no format) → narrated text.
	_, txtOut := mcpCall(t, h, "Bearer nbg_good",
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"bounce_rate","arguments":{"days":30,"min_pushes":1}}}`)
	if !strings.Contains(mcpResultText(t, txtOut), "33%") {
		t.Errorf("default should be narrated text with 33%%: %s", mcpResultText(t, txtOut))
	}
	// format=json → structured JSON envelope as the result text.
	code, out := mcpCall(t, h, "Bearer nbg_good",
		`{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"bounce_rate","arguments":{"days":30,"min_pushes":1,"format":"json"}}}`)
	if code != http.StatusOK {
		t.Fatalf("code %d", code)
	}
	text := mcpResultText(t, out)
	var env struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(text), &env); err != nil {
		t.Fatalf("format=json should yield JSON text: %v\n%s", err, text)
	}
	if len(env.Data) == 0 || env.Data[0]["repo"] != "demo" {
		t.Errorf("structured data wrong: %s", text)
	}
}
