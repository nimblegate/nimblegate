// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package main

import (
	"fmt"
	"os"

	"nimblegate/internal/commands"
	"nimblegate/internal/version"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "--version", "-v", "version":
		fmt.Printf("nimblegate %s\n", version.Resolved())
	case "check":
		os.Exit(commands.Check(args))
	case "init":
		os.Exit(commands.Init(args))
	case "lint":
		os.Exit(commands.Lint(args))
	case "status":
		os.Exit(commands.Status(args))
	case "tasks":
		os.Exit(commands.Tasks(args))
	case "review":
		os.Exit(commands.Review(args))
	case "slice":
		os.Exit(commands.Slice(args))
	case "scan":
		os.Exit(commands.Scan(args))
	case "dashboard":
		os.Exit(commands.Dashboard(args))
	case "fix":
		os.Exit(commands.Fix(args))
	case "gateway":
		os.Exit(commands.Gateway(args))
	case "watch":
		os.Exit(commands.Watch(args))
	case "audit":
		os.Exit(commands.Audit(args))
	case "git":
		os.Exit(commands.Git(args))
	case "cmd":
		os.Exit(commands.Cmd(args))
	case "shell":
		os.Exit(commands.Shell(args))
	case "list":
		os.Exit(commands.List(args))
	case "info":
		os.Exit(commands.Info(args))
	case "enable":
		os.Exit(commands.Enable(args))
	case "disable":
		os.Exit(commands.Disable(args))
	case "whitelist":
		os.Exit(commands.Whitelist(args))
	case "incident":
		os.Exit(commands.Incident(args))
	case "intro":
		os.Exit(commands.Intro(args))
	case "patterns":
		os.Exit(commands.Patterns(args))
	case "frame":
		os.Exit(commands.Frame(args))
	case "frames":
		os.Exit(commands.Frames(args))
	case "kits":
		os.Exit(commands.Kits(args))
	case "history":
		os.Exit(commands.History(args))
	case "doctor":
		os.Exit(commands.Doctor(args))
	case "setup":
		os.Exit(commands.Setup(args))
	case "purge":
		os.Exit(commands.Purge(args))
	case "pause":
		os.Exit(commands.Pause(args))
	case "resume":
		os.Exit(commands.Resume(args))
	case "migrate-config":
		os.Exit(commands.MigrateConfig(args))
	case "--help", "-h", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "nimblegate: unknown subcommand %q\n", cmd)
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `nimblegate - git-gated rule enforcement

Usage:
  nimblegate <subcommand> [args...]

Subcommands:
  check           Run all enabled frames against the current project (CLI trigger)
  init            Scaffold appframes.toml + .appframes/ in the current project
  lint            Validate every frame (stdlib + project) without running checks
  list            Browse loaded frames; filter by --group / --tag; --json output
  info            Show one frame's full details (frontmatter + body)
  enable          Add a frame ID / @group / wildcard to appframes.toml enabled list
  disable         Remove a frame ID / @group / wildcard from appframes.toml enabled list
  whitelist       Manage the project whitelist (subcommand: list)
  incident        Capture footguns as drafts, promote them into new frames (subcommands: new, list, promote)
  intro           Print the rich project-context banner (what nimblegate is, active frames, anti-bypass rules)
  patterns        List patterns or view one (subcommands: list, view <id>)
  frame           Frame management (subcommands: test <id> [--write-grade] | archive <id> [--reason ... --move] | revive <id> [--move])
  history         Query the historical (archived/deprecated) frame pool (subcommands: list, view <id>, search <query>, check)
  doctor          Quick health check - verifies binary + stdlib + project + audit-log + selection runner are functional
  setup           Interactive install: shim + PATH edit + verification ([--yes | --dry-run | --check])
  purge           Full uninstall: remove shims + PATH edit + ~/.appframes/ ([--yes | --dry-run | --keep-config])
  pause           Suspend nimblegate enforcement (--global | --project [--reason "text"])
  resume          Re-enable nimblegate enforcement ([--global | --project | --all])
  status          Summarize recent audit log activity
  watch           Live-tail the audit log
  audit           Audit-log management (subcommands: reset)
  gateway         Run/administer the policy gateway. Subcommands:
                    repos:    add, archive, delete, restore, rescan, migrate-layout
                    serving:  dashboard, setup-token, relay-service, pre-receive, post-receive
                    hardening: harden-sshd, shell, access
                    ops:      token, bind, tls-setup, analytics, benchmark
  git             Internal: invoked by the git-wrap shell function
  cmd             Internal: invoked by command-wrap shell functions (apt, apt-get, etc.)
  shell           Install/print/uninstall the command-wrap shell functions
  version         Print version and exit
  help            Print this message`)
}
