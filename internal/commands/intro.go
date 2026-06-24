// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"flag"
	"fmt"
	"os"

	"nimblegate/internal/banner"
	"nimblegate/internal/config"
	"nimblegate/internal/frames"
	"nimblegate/internal/paths"
	"nimblegate/internal/stdlib"
)

// Intro implements `nimblegate intro` - always prints the rich first-time
// banner regardless of marker state. Useful for:
//
//   - AI agents that want to load project context at session start
//     ("first thing I'll do is `nimblegate intro` to learn what this
//     project gates").
//   - Users / new contributors who want to re-read the intro.
//   - Onboarding documentation that says "run `nimblegate intro` to see
//     what this project enforces".
//
// Unlike the auto-shown intro (from a gated invocation), this does NOT
// update the seen-marker. Running `nimblegate intro` doesn't "use up"
// the first-time encounter.
//
// Flag --for-agent emits the terse agent-targeted brief instead of the
// rich human banner. Use when an AI agent needs to load project context
// at session start without burning tokens on decorative ASCII.
// Added 2026-05-21 with Slice 11.
func Intro(args []string) int {
	fs := flag.NewFlagSet("intro", flag.ExitOnError)
	forAgent := fs.Bool("for-agent", false, "emit terse agent-targeted brief (no banner; comment-prefixed; ~25 lines)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	cwd, err := os.Getwd()
	if err != nil {
		return 2
	}
	root, err := paths.FindProjectRoot(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate intro: %v\nHint: run `nimblegate init` here.\n", err)
		return 2
	}

	stdlibFrames, _ := stdlib.Load()
	projectFrames, _ := frames.LoadFromDir(paths.AppframesDir(root))
	cfg, _ := config.LoadProject(paths.ConfigPath(root))

	frameCount := frameCountFromConfig(cfg, stdlibFrames, projectFrames)

	bctx := banner.Context{
		ProjectRoot: root,
		ProjectName: banner.DefaultProjectName(root),
		FrameCount:  frameCount,
	}
	bctx.DesignDocPath, bctx.FutureDocPath = banner.DetectDocPaths(root)

	if *forAgent {
		banner.RenderIntroAgent(os.Stdout, bctx)
	} else {
		banner.RenderIntroForced(os.Stdout, bctx)
	}
	return 0
}

// frameCountFromConfig counts how many distinct frames are actually
// enabled. Conservative: counts loaded stdlib + project frames that
// match an entry in the flat enabled list.
func frameCountFromConfig(cfg config.ProjectConfig, stdlibFrames, projectFrames []frames.Frame) int {
	enabled := cfg.Frames.Enabled
	if len(enabled) == 0 {
		return 0
	}
	seen := map[string]bool{}
	for _, f := range stdlibFrames {
		if frameEnabledInList(f.ID(), enabled, nil) {
			seen[f.ID()] = true
		}
	}
	for _, f := range projectFrames {
		if frameEnabledInList(f.ID(), enabled, nil) {
			seen[f.ID()] = true
		}
	}
	return len(seen)
}
