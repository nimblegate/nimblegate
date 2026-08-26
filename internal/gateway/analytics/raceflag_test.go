// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

//go:build !race

package analytics

// raceInstrumented reports whether the binary was built with -race. The
// detector adds roughly 15-20% to Stats' runtime, which is enough to trip a
// latency budget tuned for an uninstrumented run.
const raceInstrumented = false
