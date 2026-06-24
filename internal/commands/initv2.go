// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// AxisDecision is the resolved per-axis pick after layering flag overrides
// over auto-detection. PromptFramework/PromptPlatform are set when the axis
// is ambiguous (conflict between two high-confidence signals, no flag
// supplied) - the caller decides whether to prompt interactively or error
// out based on whether stdin is a TTY.
type AxisDecision struct {
	Framework       string
	Platform        string
	PromptFramework bool
	PromptPlatform  bool
}

// ResolveAxes layers flag overrides over auto-detection per spec §6.3 + §6.4:
//
//   - Manual override always wins (§6.4): non-empty flagFw / flagPf becomes
//     the pick, no prompt, detection ignored for that axis.
//   - Clean detection (single high-confidence or fallback) passes through.
//   - Conflict (≥2 high-confidence signals) with no flag → Prompt* set.
//   - No signal at all (ConfidenceNone) → axis stays empty, no prompt
//     (axis is optional in v2; not every project has a framework).
//
// Pure - no I/O. Init wiring decides what to do with the prompt flags
// (interactive read vs error-with-suggestion).
func ResolveAxes(detected AxisRecommendation, flagFw, flagPf string) AxisDecision {
	d := AxisDecision{}

	conflictFw := false
	conflictPf := false
	for _, c := range detected.Conflicts {
		switch c {
		case "framework":
			conflictFw = true
		case "platform":
			conflictPf = true
		}
	}

	switch {
	case flagFw != "":
		d.Framework = flagFw
	case conflictFw:
		d.PromptFramework = true
	default:
		d.Framework = detected.Framework
	}

	switch {
	case flagPf != "":
		d.Platform = flagPf
	case conflictPf:
		d.PromptPlatform = true
	default:
		d.Platform = detected.Platform
	}

	return d
}

// PromptAxis reads a numbered choice for one axis from r and returns the
// selected value. Writes the prompt + candidate list to w. Used by init's
// interactive (TTY) path.
//
// The candidate list comes from detected.CandidatesByAxis[axisName] - the
// high-confidence signals that fired. Returns the chosen value or an error
// (EOF, invalid input after 3 retries, etc.). The caller should have
// already confirmed stdin is a TTY before calling.
func PromptAxis(r io.Reader, w io.Writer, axisName string, candidates []string) (string, error) {
	if len(candidates) == 0 {
		return "", fmt.Errorf("no candidates for axis %q", axisName)
	}
	sorted := append([]string{}, candidates...)
	sort.Strings(sorted)

	br := bufio.NewReader(r)
	for attempt := 0; attempt < 3; attempt++ {
		fmt.Fprintf(w, "Multiple %s signals detected. Pick one:\n", axisName)
		for i, c := range sorted {
			fmt.Fprintf(w, "  %d) %s\n", i+1, c)
		}
		fmt.Fprintf(w, "Enter 1-%d: ", len(sorted))

		line, err := br.ReadString('\n')
		if err != nil {
			return "", err
		}
		line = strings.TrimSpace(line)
		n, err := strconv.Atoi(line)
		if err != nil || n < 1 || n > len(sorted) {
			fmt.Fprintf(w, "invalid choice %q; try again.\n", line)
			continue
		}
		return sorted[n-1], nil
	}
	return "", fmt.Errorf("no valid choice after 3 attempts")
}

// NonInteractiveAmbiguityError formats the helpful error message printed
// when init detects axis ambiguity but stdin isn't a TTY (CI run, scripted
// invocation). The message lists the candidates per axis and suggests the
// flag form the operator should add to their command.
func NonInteractiveAmbiguityError(decision AxisDecision, detected AxisRecommendation) error {
	var b strings.Builder
	b.WriteString("nimblegate init: ambiguity detected and stdin is not a TTY.\n")
	b.WriteString("Re-run with explicit flags to pick:\n")
	if decision.PromptFramework {
		cands := append([]string{}, detected.CandidatesByAxis["framework"]...)
		sort.Strings(cands)
		fmt.Fprintf(&b, "  --framework=<one of: %s>\n", strings.Join(cands, ", "))
	}
	if decision.PromptPlatform {
		cands := append([]string{}, detected.CandidatesByAxis["platform"]...)
		sort.Strings(cands)
		fmt.Fprintf(&b, "  --platform=<one of: %s>\n", strings.Join(cands, ", "))
	}
	return fmt.Errorf("%s", b.String())
}
