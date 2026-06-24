// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package linters

import (
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"nimblegate/internal/config"
	"nimblegate/internal/engine"
)

// customLinter is a user-defined linter ([linters.<name>] with no built-in
// adapter): nimblegate runs cfg.Command + cfg.Args (+ cfg.Patterns as trailing
// args) and parses each output line with cfg.Regex's named groups (file, line,
// msg). This is the "bring any CLI linter without writing Go" path.
type customLinter struct {
	name string
}

func (c customLinter) ID() string { return frameID(c.name) }

func (c customLinter) Run(projectRoot string, cfg config.LinterConfig, _ []string) engine.CheckResult {
	id := c.ID()
	if cfg.Command == "" {
		return skipResult(id, c.name+": skipped (no `command` configured)")
	}
	if _, err := exec.LookPath(cfg.Command); err != nil {
		return skipResult(id, c.name+": skipped ("+cfg.Command+" not found on PATH)")
	}
	if cfg.Regex == "" {
		return skipResult(id, c.name+": skipped (no `regex` configured to parse output - need named groups file/line/msg)")
	}
	re, err := regexp.Compile(cfg.Regex)
	if err != nil {
		return skipResult(id, c.name+": skipped (invalid regex: "+err.Error()+")")
	}

	args := append([]string{}, cfg.Args...)
	args = append(args, cfg.Patterns...) // patterns passed as trailing args (literal - no shell glob expansion)
	cmd := exec.Command(cfg.Command, args...)
	cmd.Dir = linterRoot(projectRoot, cfg)
	out, _ := cmd.CombinedOutput() // non-zero exit when findings exist is expected
	hits := parseRegexOutput(string(out), projectRoot, re)
	return buildResult(id, c.name, hits, resolveOutcome(cfg.Severity), cfg.Disable)
}

// parseRegexOutput applies a named-group regex (groups: file, line, msg) to each
// output line, building a hit per match. Lines that don't match are ignored.
func parseRegexOutput(out, projectRoot string, re *regexp.Regexp) []engine.Hit {
	idx := map[string]int{}
	for i, n := range re.SubexpNames() {
		if n != "" {
			idx[n] = i
		}
	}
	group := func(m []string, name string) string {
		if i, ok := idx[name]; ok && i < len(m) {
			return m[i]
		}
		return ""
	}
	var hits []engine.Hit
	for _, line := range strings.Split(out, "\n") {
		m := re.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		file := group(m, "file")
		msg := group(m, "msg")
		if file == "" && msg == "" {
			continue
		}
		ln := 0
		if n, err := strconv.Atoi(group(m, "line")); err == nil {
			ln = n
		}
		hits = append(hits, engine.Hit{File: relTo(file, projectRoot), Line: ln, Label: msg})
	}
	return hits
}
