// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"nimblegate/internal/paths"
	"nimblegate/internal/tasks"
)

// Fix turns tracked findings into agent-ready fix tasks. By default it prints a
// fix prompt per selected task (hand it to your agent). With --agent it pipes
// each prompt to YOUR agent command (e.g. `claude -p`) and then re-runs
// `nimblegate check` to verify the finding actually cleared - a fix that doesn't
// resolve the finding is reported, not trusted. nimblegate never calls a model
// itself; the agent is external (deterministic OSS core).
func Fix(args []string) int {
	fs := flag.NewFlagSet("fix", flag.ExitOnError)
	agent := fs.String("agent", "", "command to dispatch each fix prompt to (prompt on stdin), e.g. \"claude -p\"; then re-verify")
	dangerous := fs.Bool("dangerous", false, "fix all open dangerous (BLOCK) findings")
	all := fs.Bool("all", false, "fix all open findings")
	// Go's flag stops at the first positional, so peel a leading <id> off
	// first - lets `fix <id> --agent X` work as well as `fix --agent X <id>`.
	idArg, rest := "", args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		idArg, rest = args[0], args[1:]
	}
	_ = fs.Parse(rest)
	if idArg == "" {
		idArg = fs.Arg(0)
	}

	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate fix: getwd: %v\n", err)
		return 2
	}
	root, err := paths.FindProjectRoot(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate fix: %v\nHint: run `nimblegate init` here.\n", err)
		return 2
	}
	ledger, err := tasks.Load(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate fix: %v\n", err)
		return 2
	}

	targets, code := selectFixTargets(ledger, idArg, *all, *dangerous)
	if code != 0 {
		return code
	}
	if len(targets) == 0 {
		fmt.Println("Nothing to fix, no matching open tasks.")
		return 0
	}

	if *agent == "" {
		for i, t := range targets {
			if i > 0 {
				fmt.Println("\n---")
			}
			fmt.Printf("# task %s\n%s\n", t.ID, fixPrompt(t))
		}
		fmt.Fprintf(os.Stderr, "\n%d fix prompt(s) above. Pipe to your agent, or re-run with --agent \"<cmd>\" to dispatch + verify.\n", len(targets))
		return 0
	}

	// Dispatch to the user's agent, then verify each cleared.
	for _, t := range targets {
		fmt.Printf("→ fixing %s: %s (%s)\n", t.FrameID, locTask(t), t.ID)
		if err := dispatchAgent(*agent, fixPrompt(t), root); err != nil {
			fmt.Fprintf(os.Stderr, "  agent error: %v\n", err)
		}
	}
	return verifyFixes(root, targets)
}

func selectFixTargets(l *tasks.Ledger, id string, all, dangerous bool) ([]*tasks.Task, int) {
	switch {
	case all:
		return l.OpenTasks(), 0
	case dangerous:
		var out []*tasks.Task
		for _, t := range l.OpenTasks() {
			if t.Severity == "BLOCK" {
				out = append(out, t)
			}
		}
		return out, 0
	case id != "":
		t, err := l.Find(id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "nimblegate fix: %v\n", err)
			return nil, 2
		}
		return []*tasks.Task{t}, 0
	default:
		fmt.Fprintln(os.Stderr, "usage: nimblegate fix <task-id> | --dangerous | --all  [--agent \"<cmd>\"]")
		return nil, 2
	}
}

func fixPrompt(t *tasks.Task) string {
	return fmt.Sprintf(`Fix this nimblegate finding with a minimal, correct change. Do NOT disable the
check, add a suppression comment, or whitelist it: the finding must stop firing
on its own after your edit.

  Frame:    %s  (%s)
  Location: %s
  Issue:    %s

Edit only what is needed at that location; don't introduce new findings.`,
		t.FrameID, t.Severity, locTask(t), t.Label)
}

func locTask(t *tasks.Task) string {
	if t.File == "" {
		return "(project-level)"
	}
	if t.Line > 0 {
		return fmt.Sprintf("%s:%d", t.File, t.Line)
	}
	return t.File
}

// dispatchAgent runs the user's agent command with the prompt on stdin, in the
// project root. The command is split on spaces (e.g. `claude -p`).
func dispatchAgent(agentCmd, prompt, root string) error {
	parts := strings.Fields(agentCmd)
	if len(parts) == 0 {
		return fmt.Errorf("empty --agent command")
	}
	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Dir = root
	cmd.Stdin = strings.NewReader(prompt)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// verifyFixes re-runs `nimblegate check` (updates the ledger), then reports which
// targeted tasks actually cleared. Returns 1 if any remain open.
func verifyFixes(root string, targets []*tasks.Task) int {
	exe, err := os.Executable()
	if err != nil {
		exe = "nimblegate"
	}
	check := exec.Command(exe, "check")
	check.Dir = root
	_ = check.Run() // reconciles the ledger; output suppressed (we read state below)

	ledger, _ := tasks.Load(root)
	resolved, remaining := 0, 0
	fmt.Println("\nVerification (re-ran nimblegate check):")
	for _, t := range targets {
		cur := ledger.Tasks[t.ID]
		if cur == nil || cur.Status == tasks.StatusResolved {
			resolved++
			fmt.Printf("  ✓ resolved: %s %s\n", t.FrameID, locTask(t))
		} else {
			remaining++
			fmt.Printf("  ✗ still firing: %s %s (fix didn't clear it; review)\n", t.FrameID, locTask(t))
		}
	}
	fmt.Printf("\n%d resolved, %d still open.\n", resolved, remaining)
	if remaining > 0 {
		return 1
	}
	return 0
}
