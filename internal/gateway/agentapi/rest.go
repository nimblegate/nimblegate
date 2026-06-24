// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package agentapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// corsHeaders lets browser-based MCP clients (e.g. the llama.cpp webui)
// reach the agent endpoints cross-origin. Safe with "*": auth is a bearer
// header, never cookies, so cross-origin callers still need a valid token.
// Preflight OPTIONS must succeed WITHOUT auth - browsers never attach the
// Authorization header to preflights.
func corsHeaders(w http.ResponseWriter, r *http.Request) (preflight bool) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	// "*" covers any header the client adds (Content-Type, mcp-protocol-version,
	// …) for non-credentialed requests, but the CORS spec excludes Authorization
	// from the wildcard - so it must be listed explicitly alongside it.
	w.Header().Set("Access-Control-Allow-Headers", "Authorization, *")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return true
	}
	return false
}

// RESTHandler serves GET /api/v1/{stats,bounce-rate,top-rules,recurring,decisions,time-saved,changes}.
// Bearer-token authenticated, read-only (GET enforced), JSON responses.
func (s *Service) RESTHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if corsHeaders(w, r) {
			return
		}
		if s == nil || s.Verify == nil {
			httpErr(w, http.StatusServiceUnavailable, "agent API requires auth mode setup-token")
			return
		}
		if r.Method != http.MethodGet {
			httpErr(w, http.StatusMethodNotAllowed, "read-only API: GET only")
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

		p := paramsFromQuery(r)
		var res Result
		switch strings.TrimPrefix(r.URL.Path, "/api/v1/") {
		case "stats":
			res, err = s.GateStats(p)
		case "bounce-rate":
			res, err = s.BounceRate(p)
		case "top-rules":
			res, err = s.TopRules(p)
		case "recurring":
			res, err = s.Recurring(p)
		case "decisions":
			res, err = s.Decisions(p)
		case "time-saved":
			res, err = s.TimeSaved(p)
		case "changes":
			res, err = s.WhatChanged(p)
		default:
			httpErr(w, http.StatusNotFound, "unknown endpoint")
			return
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "agentapi:", err)
			httpErr(w, http.StatusServiceUnavailable, "analytics unavailable")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(struct {
			Data  any      `json:"data"`
			Notes []string `json:"notes,omitempty"`
		}{Data: res.JSON, Notes: res.Notes})
	})
}

func bearer(r *http.Request) (string, bool) {
	h := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(h) < 7 || !strings.EqualFold(h[:7], "Bearer ") {
		return "", false
	}
	tok := strings.TrimSpace(h[7:])
	return tok, tok != ""
}

func paramsFromQuery(r *http.Request) Params {
	q := r.URL.Query()
	atoi := func(k string) int { n, _ := strconv.Atoi(q.Get(k)); return n }
	return Params{
		Repo:      q.Get("repo"),
		Days:      atoi("days"),
		Severity:  q.Get("severity"),
		Result:    q.Get("result"),
		MinPushes: atoi("min"),
		Limit:     atoi("limit"),
		Path:      q.Get("path"),
		Query:     q.Get("query"),
	}
}

func httpErr(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
