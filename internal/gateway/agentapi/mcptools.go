// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package agentapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// argsSchema is shared: every tool takes the same optional, clamped params.
const argsSchema = `{"type":"object","properties":{
"repo":{"type":"string","description":"repository name; omit for all repos"},
"days":{"type":"integer","description":"window in days (default 30, max 365)"},
"severity":{"type":"string","description":"filter findings: BLOCK, ERROR, WARN or INFO"},
"result":{"type":"string","description":"filter decisions: accepted or rejected"},
"min_pushes":{"type":"integer","description":"bounce_rate: minimum pushes per repo (default 5)"},
"limit":{"type":"integer","description":"max rows (default 10, max 50)"},
"path":{"type":"string","description":"what_changed: limit to commits touching this path"},
"query":{"type":"string","description":"what_changed: keyword to grep commit subjects/messages"},
"format":{"type":"string","description":"\"text\" (default, narrated) or \"json\" (structured data to compute/analyze on)"}}}`

// MCPHandler returns the /mcp endpoint: bearer-authenticated MCP (JSON-RPC
// 2.0 over HTTP) exposing the agent tools.
func (s *Service) MCPHandler() http.Handler {
	srv := newMCPServer("nimblegate-agent-api", "v1")
	reg := func(name, desc string, fn func(Params) (Result, error)) {
		srv.Register(Tool{
			Name:        name,
			Description: desc + " Numbers come from the gateway's frame-validated decision log; cite the period.",
			InputSchema: json.RawMessage(argsSchema),
			Handler: func(args json.RawMessage) (string, error) {
				var p Params
				if len(args) > 0 {
					if err := json.Unmarshal(args, &p); err != nil {
						return "", fmt.Errorf("invalid arguments: %v", err)
					}
				}
				res, err := fn(p)
				if err != nil {
					return "", err
				}
				// format=json → the structured envelope as the result text, so
				// any client (including a small local model) can compute on the
				// numbers instead of parsing prose. Default stays narrated text.
				if strings.EqualFold(p.Format, "json") {
					b, err := json.MarshalIndent(struct {
						Data  any      `json:"data"`
						Notes []string `json:"notes,omitempty"`
					}{res.JSON, res.Notes}, "", "  ")
					if err != nil {
						return "", err
					}
					return string(b), nil
				}
				return res.Text, nil
			},
		})
	}
	reg("gate_stats", "Summary of gate activity: decisions, accept/reject counts, per-repo activity, top firing rules.", s.GateStats)
	reg("bounce_rate", "Repos ranked by share of pushes the gate rejected - where code bounces back the most.", s.BounceRate)
	reg("top_rules", "Which rules (frames) produced the most findings, optionally filtered by severity.", s.TopRules)
	reg("recurring_findings", "Findings that keep coming back push after push, for one repo.", s.Recurring)
	reg("decisions", "Recent individual gate decisions with their findings - the receipts behind any statistic.", s.Decisions)
	reg("time_saved", "Estimated debugging time the gate prevented: distinct blocking findings weighted by per-frame hours-per-hit. Actual = blocked-and-fixed; modeled = conservative upper bound.", s.TimeSaved)
	reg("what_changed", "Recent commits in a repo (or all repos) - what changed and where to look, with the gate's verdict on each pushed tip. To look at ONE repository, pass its name as 'repo' (e.g. repo=appframes), NOT as 'query'. 'query' greps commit messages; 'path' limits to a file path; 'days' sets the window.", s.WhatChanged)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if corsHeaders(w, r) {
			return
		}
		if s == nil || s.Verify == nil {
			httpErr(w, http.StatusServiceUnavailable, "agent API requires auth mode setup-token")
			return
		}
		tok, ok := bearer(r)
		if !ok {
			httpErr(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		valid, err := s.Verify(tok)
		if err != nil {
			httpErr(w, http.StatusInternalServerError, "token verification failed")
			return
		}
		if !valid {
			httpErr(w, http.StatusForbidden, "invalid or revoked token")
			return
		}
		if !s.allow(tok) {
			w.Header().Set("Retry-After", "60")
			httpErr(w, http.StatusTooManyRequests, "rate limit exceeded")
			return
		}
		srv.ServeHTTP(w, r)
	})
}
