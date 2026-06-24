// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

// Package agentapi - minimal MCP JSON-RPC 2.0 server (Streamable HTTP,
// plain application/json responses - no SSE). Ported from the sibling
// ai-assistant project; CORS removed (agents are not browsers here).
package agentapi

import (
	"encoding/json"
	"io"
	"net/http"
)

// defaultProtocolVersion is used only if the client sends no protocolVersion.
const defaultProtocolVersion = "2024-11-05"

// Tool is a registered MCP tool. Handler returns the tool's text output; a
// non-nil error is surfaced to the model as an isError tool result (not a
// protocol-level error).
type Tool struct {
	Name        string
	Description string
	InputSchema json.RawMessage
	Handler     func(args json.RawMessage) (string, error)
}

// mcpServer serves MCP JSON-RPC over HTTP.
type mcpServer struct {
	name    string
	version string

	tools []Tool
	index map[string]Tool
}

func newMCPServer(name, version string) *mcpServer {
	return &mcpServer{name: name, version: version, index: map[string]Tool{}}
}

// Register adds a tool. Call before serving.
func (s *mcpServer) Register(t Tool) {
	s.tools = append(s.tools, t)
	s.index[t.Name] = t
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"` // absent => notification
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

func (s *mcpServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	var req rpcRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, rpcResponse{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "parse error"}})
		return
	}

	// Notifications (no id) are acknowledged with 202 and no body.
	if len(req.ID) == 0 {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	writeJSON(w, s.handle(req))
}

func (s *mcpServer) handle(req rpcRequest) rpcResponse {
	resp := rpcResponse{JSONRPC: "2.0", ID: req.ID}
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		// params is optional; on absence/error the zero value drives the fallback below
		_ = json.Unmarshal(req.Params, &p)
		pv := p.ProtocolVersion
		if pv == "" {
			pv = defaultProtocolVersion
		}
		resp.Result = map[string]any{
			"protocolVersion": pv,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": s.name, "version": s.version},
		}
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		tools := make([]map[string]any, 0, len(s.tools))
		for _, t := range s.tools {
			tools = append(tools, map[string]any{
				"name":        t.Name,
				"description": t.Description,
				"inputSchema": t.InputSchema,
			})
		}
		resp.Result = map[string]any{"tools": tools}
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			resp.Error = &rpcError{Code: -32602, Message: "invalid params"}
			break
		}
		t, ok := s.index[p.Name]
		if !ok {
			resp.Error = &rpcError{Code: -32602, Message: "unknown tool: " + p.Name}
			break
		}
		out, err := t.Handler(p.Arguments)
		if err != nil {
			resp.Result = toolResult(err.Error(), true)
		} else {
			resp.Result = toolResult(out, false)
		}
	default:
		resp.Error = &rpcError{Code: -32601, Message: "method not found: " + req.Method}
	}
	return resp
}

func toolResult(text string, isError bool) map[string]any {
	return map[string]any{
		"content": []map[string]any{{"type": "text", "text": text}},
		"isError": isError,
	}
}

func writeJSON(w http.ResponseWriter, resp rpcResponse) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
