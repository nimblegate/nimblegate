// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"

	"nimblegate/internal/paths"
	"nimblegate/internal/stdlib"
)

// Enable implements `nimblegate enable <id>`. Adds the entry to
// appframes.toml's [frames] enabled list, sorts the list, and rewrites
// the file in place. Preserves comments + non-enabled sections.
func Enable(args []string) int {
	return enableOrDisable(args, true)
}

// Disable implements `nimblegate disable <id>`. Removes the entry from
// the enabled list. If the resulting list still matches the frame via
// a wildcard, the user is warned and the action proceeds.
func Disable(args []string) int {
	return enableOrDisable(args, false)
}

func enableOrDisable(args []string, enable bool) int {
	verb := "enable"
	if !enable {
		verb = "disable"
	}
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "nimblegate %s: frame ID required\n", verb)
		fmt.Fprintf(os.Stderr, "usage: nimblegate %s <category/name | category/* | *>\n", verb)
		return 2
	}
	target := args[0]

	cwd, _ := os.Getwd()
	root, err := paths.FindProjectRoot(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate %s: %v\nHint: run `nimblegate init` here.\n", verb, err)
		return 2
	}

	// Validate target: known frame ID or wildcard pattern.
	if err := validateEnableTarget(target); err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate %s: %v\n", verb, err)
		return 2
	}

	cfgPath := paths.ConfigPath(root)
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate %s: read %s: %v\n", verb, cfgPath, err)
		return 2
	}

	updated, changed, err := rewriteEnabledList(string(data), target, enable)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate %s: %v\n", verb, err)
		return 2
	}
	if !changed {
		if enable {
			fmt.Printf("Already enabled: %s\n", target)
		} else {
			fmt.Printf("Already disabled (not in enabled list): %s\n", target)
		}
		return 0
	}
	if err := os.WriteFile(cfgPath, []byte(updated), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate %s: write %s: %v\n", verb, cfgPath, err)
		return 2
	}
	if enable {
		fmt.Printf("✓ Enabled: %s\n", target)
	} else {
		fmt.Printf("✓ Disabled: %s\n", target)
	}
	return 0
}

// validateEnableTarget refuses targets that don't resolve to anything.
// A typo here would silently bloat the enabled list with a dead entry -
// catching it now matches the fail-closed posture from whitelist.
func validateEnableTarget(target string) error {
	if target == "*" {
		return nil
	}
	if strings.HasPrefix(target, "@") {
		// @-prefixed group syntax is no longer supported. Kits replace groups;
		// use `nimblegate kits apply <kit>` to enable a curated frame set.
		return fmt.Errorf("%s", removedGroupSyntaxEnableMsg(target))
	}
	// Category wildcard or exact ID.
	stdlibFrames, _ := stdlib.Load()
	for _, f := range stdlibFrames {
		if f.ID() == target {
			return nil
		}
	}
	if strings.HasSuffix(target, "/*") {
		prefix := strings.TrimSuffix(target, "*")
		for _, f := range stdlibFrames {
			if strings.HasPrefix(f.ID(), prefix) {
				return nil
			}
		}
	}
	return fmt.Errorf("unknown target %q (try `nimblegate list` to see available frames)", target)
}

// removedGroupSyntaxEnableMsg returns the migration-error message shown
// when a user attempts `nimblegate enable @group-name`.
func removedGroupSyntaxEnableMsg(target string) string {
	return fmt.Sprintf(`@-prefixed group syntax is no longer supported: %s

Use kits instead:
  nimblegate kits apply core
  nimblegate kits apply web-app
  nimblegate kits apply cf-pages-project
  nimblegate kits apply cf-workers-project
  nimblegate kits apply security-strict

Or enable individual frames by ID:
  nimblegate enable <category/frame-name>

Run `+"`nimblegate kits list`"+` to see all available kits.
`, target)
}

// enabledArrayRegex matches the `enabled = [...]` array literal in
// appframes.toml's [frames] section. Tolerates whitespace, comments at
// line ends inside the array, and trailing commas. The array body is
// captured for in-place rewrite.
//
// Limitations (deliberate): if the user has an unusual multi-section
// layout (two [frames] tables, mixed inline-array+table form, etc.)
// this regex may not find the right place. That's caught at parse time
// downstream and surfaced as a clean error to the user.
var enabledArrayRegex = regexp.MustCompile(`(?s)(\[frames\][^\[]*?enabled\s*=\s*\[)(.*?)(\])`)

// rewriteEnabledList edits the enabled array of an appframes.toml document
// to add/remove `target`. Returns the new document, a bool indicating
// whether any change occurred, and an error if the document doesn't
// match the expected shape.
func rewriteEnabledList(doc, target string, enable bool) (string, bool, error) {
	matches := enabledArrayRegex.FindStringSubmatchIndex(doc)
	if matches == nil {
		return "", false, fmt.Errorf("could not find [frames] enabled list in appframes.toml: file shape too irregular for in-place rewrite; edit it manually")
	}
	bodyStart, bodyEnd := matches[4], matches[5]
	body := doc[bodyStart:bodyEnd]

	// Parse the existing entries by stripping comments / whitespace and
	// splitting on commas. Each entry is whatever's inside the quotes.
	entries := parseEnabledBody(body)
	idx := -1
	for i, e := range entries {
		if e == target {
			idx = i
			break
		}
	}
	switch {
	case enable && idx == -1:
		entries = append(entries, target)
	case !enable && idx != -1:
		entries = append(entries[:idx], entries[idx+1:]...)
	default:
		return doc, false, nil
	}
	sort.Strings(entries)

	// Render the new body. Match the existing indentation if obvious.
	indent := "    "
	if strings.Contains(body, "\n  \"") {
		indent = "  "
	}
	var b strings.Builder
	b.WriteString("\n")
	for _, e := range entries {
		b.WriteString(indent)
		b.WriteString(`"`)
		b.WriteString(e)
		b.WriteString(`",`)
		b.WriteString("\n")
	}
	return doc[:bodyStart] + b.String() + doc[bodyEnd:], true, nil
}

// parseEnabledBody pulls out the quoted strings from inside the array
// literal. Strips line comments and tolerates trailing commas. Returns
// entries in the order encountered.
func parseEnabledBody(body string) []string {
	var entries []string
	for _, line := range strings.Split(body, "\n") {
		// Trim comments.
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" || line == "," {
			continue
		}
		// Find every quoted substring on this line.
		for _, q := range extractQuotedStrings(line) {
			entries = append(entries, q)
		}
	}
	// Deduplicate while preserving first-seen order.
	seen := map[string]bool{}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
	}
	return out
}

// extractQuotedStrings pulls every "..." substring from s. Handles
// adjacent entries on one line ("a", "b") naturally.
func extractQuotedStrings(s string) []string {
	var out []string
	i := 0
	for i < len(s) {
		j := strings.IndexByte(s[i:], '"')
		if j < 0 {
			break
		}
		j += i
		k := strings.IndexByte(s[j+1:], '"')
		if k < 0 {
			break
		}
		k += j + 1
		out = append(out, s[j+1:k])
		i = k + 1
	}
	return out
}
