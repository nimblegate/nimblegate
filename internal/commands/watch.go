// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
	"nimblegate/internal/paths"
)

// Watch tails the audit log, pretty-printing entries as they arrive. Exits on
// SIGINT/SIGTERM.
//
// Multi-file aware: tails audit.log AND every part file under audit.parts/.
// Files that already existed when watch started are seeked to end (skip
// backlog). Files that appear AFTER watch started are read from the
// beginning - they represent a new nimblegate invocation whose entries are
// inherently fresh.
//
// Implementation is a poll loop (no fsnotify) to stay dependency-free.
func Watch(args []string) int {
	cwd, err := os.Getwd()
	if err != nil {
		return 2
	}
	root, err := paths.FindProjectRoot(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nimblegate watch: %v\n", err)
		return 2
	}
	logPath := paths.AuditLogPath(root)
	fmt.Printf("watching %s (+ %s), Ctrl-C to stop\n",
		logPath, "audit.parts/")

	// Snapshot of files that existed at start; these get seeked to end
	// so we don't replay history. Anything appearing later is new.
	startSnapshot := map[string]bool{}
	for _, p := range engine.RotatedFiles(logPath) {
		if _, err := os.Stat(p); err == nil {
			startSnapshot[p] = true
		}
	}

	type tailed struct {
		f      *os.File
		reader *bufio.Reader
	}
	open := map[string]*tailed{}
	defer func() {
		for _, t := range open {
			_ = t.f.Close()
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	for {
		select {
		case <-stop:
			fmt.Println("\nstopped.")
			return 0
		default:
		}

		// Discover current file set; open new ones.
		current := engine.RotatedFiles(logPath)
		seen := map[string]bool{}
		for _, p := range current {
			if _, err := os.Stat(p); err != nil {
				continue
			}
			seen[p] = true
			if _, ok := open[p]; ok {
				continue
			}
			f, err := os.OpenFile(p, os.O_RDONLY, 0o644)
			if err != nil {
				continue
			}
			if startSnapshot[p] {
				// Existed at start - skip backlog.
				_, _ = f.Seek(0, io.SeekEnd)
			}
			open[p] = &tailed{f: f, reader: bufio.NewReader(f)}
		}
		// Close files that disappeared (e.g. compaction consumed them).
		for p, t := range open {
			if !seen[p] {
				_ = t.f.Close()
				delete(open, p)
			}
		}

		// Read whatever new lines exist across all tailed files.
		anyData := false
		for _, t := range open {
			for {
				line, err := t.reader.ReadString('\n')
				if line != "" {
					printPretty(os.Stdout, line)
					anyData = true
				}
				if err != nil {
					break
				}
			}
		}
		if !anyData {
			time.Sleep(200 * time.Millisecond)
		}
	}
}

func printPretty(w io.Writer, jsonLine string) {
	var e auditLine
	if err := json.Unmarshal([]byte(jsonLine), &e); err != nil {
		fmt.Fprint(w, jsonLine)
		return
	}
	emoji := "·"
	switch e.Result {
	case "PASS":
		emoji = "✓"
	case "WARN":
		emoji = "⚠"
	case "INFO":
		emoji = "ℹ"
	case "BLOCK":
		emoji = "✗"
	case "ERROR":
		emoji = "💥"
	}
	override := ""
	if e.Override {
		override = "  (OVERRIDE)"
	}
	reason := ""
	if e.Reason != "" {
		reason = "  " + frames.SanitizeForOutput(e.Reason)
	}
	fmt.Fprintf(w, "%s  %s  [%s/%s]  %s%s%s\n",
		formatWatchTimestamp(e.Timestamp),
		emoji,
		frames.SanitizeForOutput(e.Trigger),
		frames.SanitizeForOutput(e.Result),
		frames.SanitizeForOutput(e.Frame),
		reason, override)
}
