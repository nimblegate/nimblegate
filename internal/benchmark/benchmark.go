// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

// Package benchmark scores agent "cleanliness" from the gate's collected
// decision data: given a config (scored frames, FP whitelist, run→agent/task/
// stack mapping), it computes a per-agent/per-stack comparison matrix -
// convergence, cleanliness, recurrence - deterministically. It runs no agent;
// it reads already-recorded gate data. See the methodology charter in the spec.
package benchmark

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"nimblegate/internal/gateway/analytics"
)

// Config is the benchmark definition (TOML).
type Config struct {
	Scored    scoredBlock `toml:"scored"`
	Whitelist []WL        `toml:"whitelist"`
	Runs      []Run       `toml:"run"`
}

type scoredBlock struct {
	Frames []string `toml:"frames"`
}

// WL is one published false-positive exclusion: findings of Frame whose
// message contains Contains (or all of Frame when Contains is empty).
type WL struct {
	Frame    string `toml:"frame"`
	Contains string `toml:"contains"`
	Reason   string `toml:"reason"`
}

// Run maps one gateway repo to its benchmark coordinates (one repo = one run).
type Run struct {
	Repo  string `toml:"repo"`
	Agent string `toml:"agent"`
	Task  string `toml:"task"`
	Stack string `toml:"stack"`
	Rep   int    `toml:"rep"`
}

// Stat is a mean and population standard deviation over a run group.
type Stat struct {
	Mean   float64 `json:"mean"`
	StdDev float64 `json:"stddev"`
}

// Cell is one (agent, stack) comparison cell, aggregated over its tasks+reps.
type Cell struct {
	Agent         string         `json:"agent"`
	Stack         string         `json:"stack"`
	Runs          int            `json:"runs"`
	Cleanliness   Stat           `json:"cleanliness"`    // scored findings per push
	ConvergedRate float64        `json:"converged_rate"` // fraction of runs that reached a clean push
	Convergence   Stat           `json:"convergence"`    // pushes to first clean, over converged runs
	Recurrence    Stat           `json:"recurrence"`     // fraction of scored fingerprints repeating
	ByFrame       map[string]int `json:"by_frame"`       // total scored findings by frame
	Observed      map[string]int `json:"observed"`       // non-scored frame findings (descriptive)
}

// Matrix is the full result: cells sorted by stack then agent.
type Matrix struct {
	Cells []Cell `json:"cells"`
}

// LoadConfig reads and validates a benchmark TOML config.
func LoadConfig(path string) (Config, error) {
	var c Config
	if _, err := toml.DecodeFile(path, &c); err != nil {
		return c, fmt.Errorf("read config %s: %w", path, err)
	}
	if len(c.Scored.Frames) == 0 {
		return c, fmt.Errorf("config: [scored] frames is empty")
	}
	seen := map[string]bool{}
	for _, r := range c.Runs {
		if r.Repo == "" || r.Agent == "" || r.Stack == "" {
			return c, fmt.Errorf("config: run missing repo/agent/stack: %+v", r)
		}
		if seen[r.Repo] {
			return c, fmt.Errorf("config: duplicate run repo %q (one repo = one run)", r.Repo)
		}
		seen[r.Repo] = true
	}
	return c, nil
}

// runMetrics holds the per-run computed metrics before aggregation.
type runMetrics struct {
	cleanliness float64 // scored findings per push
	converged   bool
	convergeAt  float64 // pushes to first clean (valid if converged)
	recurrence  float64
	byFrame     map[string]int
	observed    map[string]int
}

// Score computes the comparison matrix from the gate data referenced by cfg.
func Score(db *analytics.DB, cfg Config) (Matrix, error) {
	scored := map[string]bool{}
	for _, f := range cfg.Scored.Frames {
		scored[f] = true
	}
	whitelisted := func(f analytics.PushFinding) bool {
		for _, w := range cfg.Whitelist {
			if w.Frame == f.FrameID && (w.Contains == "" || strings.Contains(f.Message, w.Contains)) {
				return true
			}
		}
		return false
	}

	// group key → per-run metrics.
	type key struct{ agent, stack string }
	groups := map[key][]runMetrics{}
	order := []key{}

	for _, run := range cfg.Runs {
		pushes, err := analytics.RunPushes(db, run.Repo)
		if err != nil {
			return Matrix{}, err
		}
		if len(pushes) == 0 {
			continue // no data for this run yet - excluded (never counted clean)
		}
		rm := runMetrics{byFrame: map[string]int{}, observed: map[string]int{}}
		fpPushCount := map[string]int{} // scored fingerprint → number of pushes it appeared in
		totalScored := 0
		for i, p := range pushes {
			seenFp := map[string]bool{}
			pushScored := 0
			for _, f := range p.Findings {
				if whitelisted(f) {
					continue
				}
				if scored[f.FrameID] {
					pushScored++
					totalScored++
					rm.byFrame[f.FrameID]++
					if f.Fingerprint != "" && !seenFp[f.Fingerprint] {
						fpPushCount[f.Fingerprint]++
						seenFp[f.Fingerprint] = true
					}
				} else {
					rm.observed[f.FrameID]++
				}
			}
			if pushScored == 0 && !rm.converged {
				rm.converged = true
				rm.convergeAt = float64(i + 1) // 1-based
			}
		}
		rm.cleanliness = float64(totalScored) / float64(len(pushes))
		if len(fpPushCount) > 0 {
			repeat := 0
			for _, n := range fpPushCount {
				if n >= 2 {
					repeat++
				}
			}
			rm.recurrence = float64(repeat) / float64(len(fpPushCount))
		}
		k := key{run.Agent, run.Stack}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], rm)
	}

	var m Matrix
	for _, k := range order {
		rms := groups[k]
		c := Cell{Agent: k.agent, Stack: k.stack, Runs: len(rms),
			ByFrame: map[string]int{}, Observed: map[string]int{}}
		var clean, recur, conv []float64
		converged := 0
		for _, rm := range rms {
			clean = append(clean, rm.cleanliness)
			recur = append(recur, rm.recurrence)
			if rm.converged {
				converged++
				conv = append(conv, rm.convergeAt)
			}
			for f, n := range rm.byFrame {
				c.ByFrame[f] += n
			}
			for f, n := range rm.observed {
				c.Observed[f] += n
			}
		}
		c.Cleanliness = stat(clean)
		c.Recurrence = stat(recur)
		c.Convergence = stat(conv) // over converged runs only
		c.ConvergedRate = float64(converged) / float64(len(rms))
		m.Cells = append(m.Cells, c)
	}
	sort.Slice(m.Cells, func(i, j int) bool {
		if m.Cells[i].Stack != m.Cells[j].Stack {
			return m.Cells[i].Stack < m.Cells[j].Stack
		}
		return m.Cells[i].Agent < m.Cells[j].Agent
	})
	return m, nil
}

// stat returns the mean and population standard deviation of xs ({} → 0,0).
func stat(xs []float64) Stat {
	if len(xs) == 0 {
		return Stat{}
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	mean := sum / float64(len(xs))
	var v float64
	for _, x := range xs {
		v += (x - mean) * (x - mean)
	}
	return Stat{Mean: mean, StdDev: math.Sqrt(v / float64(len(xs)))}
}
