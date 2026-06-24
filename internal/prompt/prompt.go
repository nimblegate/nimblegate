// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

// Package prompt provides simple interactive prompting primitives.
//
// The Prompter interface is small (YesNo) so setup / purge / similar
// commands can be tested with a mock prompter that returns canned answers.
// In production, Stdio() reads from os.Stdin and writes to os.Stdout.
// Always(true) / Always(false) implement --yes / --dry-run-style non-
// interactive paths.
package prompt

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// Prompter answers yes/no questions. Concrete implementations: Stdio (real
// interactive), Always (canned).
type Prompter interface {
	// YesNo presents msg and returns the user's choice. defaultYes determines
	// what an empty line (just Enter) means. Implementations may loop on
	// invalid input.
	YesNo(msg string, defaultYes bool) bool
}

// Stdio returns a Prompter that reads from os.Stdin and writes to os.Stdout.
func Stdio() Prompter {
	return &stdio{in: os.Stdin, out: os.Stdout}
}

// FromIO returns a Prompter wired to the given streams. Useful for tests.
func FromIO(in io.Reader, out io.Writer) Prompter {
	return &stdio{in: in, out: out}
}

// Always returns a Prompter that answers every question with the given value.
// Use Always(true) for --yes flows, Always(false) to abort everything (used
// by --dry-run for "I would prompt here but I'm in dry-run").
func Always(answer bool) Prompter {
	return &always{answer: answer}
}

type stdio struct {
	in  io.Reader
	out io.Writer
}

func (p *stdio) YesNo(msg string, defaultYes bool) bool {
	hint := "[y/N]"
	if defaultYes {
		hint = "[Y/n]"
	}
	sc := bufio.NewScanner(p.in)
	for {
		fmt.Fprintf(p.out, "%s %s ", msg, hint)
		if !sc.Scan() {
			// EOF or read error - treat as the default to avoid hanging in
			// pipelines that close stdin. Most setup flows want defaultYes,
			// so this is usually the safe option.
			return defaultYes
		}
		ans := strings.ToLower(strings.TrimSpace(sc.Text()))
		switch ans {
		case "":
			return defaultYes
		case "y", "yes":
			return true
		case "n", "no":
			return false
		default:
			fmt.Fprintln(p.out, "  please answer y/yes or n/no")
		}
	}
}

type always struct {
	answer bool
}

func (p *always) YesNo(msg string, defaultYes bool) bool {
	return p.answer
}
