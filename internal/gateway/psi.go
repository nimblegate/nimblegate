// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Pressure Stall Information is the kernel's own answer to "is this box short
// on memory", and it is the signal an operator needs BEFORE the OOM killer
// intervenes. Free-memory arithmetic cannot give it: a box with little free
// memory and no reclaim stalls is fine, while one with headroom that is
// constantly reclaiming is not. PSI reports the share of wall-clock time tasks
// spent stalled waiting on memory, which is the thing that actually hurts.
//
// Sizing a gateway for an agent swarm is guesswork until the first busy day,
// so this is what turns "pushes got slow and then something died" into a
// number the operator can see on /health while it is still recoverable.

// psiPaths are read in order. The cgroup file comes first: inside the shipped
// container it reports THAT container's pressure, which is what an operator
// running under a memory limit needs; the host-wide file is the fallback.
var psiPaths = []string{"/sys/fs/cgroup/memory.pressure", "/proc/pressure/memory"}

// Pressure is one PSI file: the "some" line (at least one task stalled) and the
// "full" line (every task stalled - the box is doing nothing but reclaiming).
type Pressure struct {
	SomeAvg10, SomeAvg60, SomeAvg300 float64
	FullAvg10, FullAvg60, FullAvg300 float64
}

// MemoryPressure is what /health renders. Unavailable is the normal case on
// kernels before 4.20, on builds without CONFIG_PSI, and on distros that ship
// it behind the psi=1 boot flag - so it must read as "no data", never as "fine".
type MemoryPressure struct {
	Available bool
	Source    string // the file it came from
	Pressure  Pressure
}

// Thresholds. "some avg10" is the early warning: tasks are already waiting on
// memory, but the box is still doing useful work. "full avg10" means everything
// stopped, which on a gateway means pushes are stalling and the OOM killer is
// the next event, so it trips at a much lower number.
const (
	pressureSomeWarnPct = 10.0
	pressureFullWarnPct = 5.0
)

// Stalled reports whether the box is under enough memory pressure that an
// operator should act - add RAM, cap concurrency, or lower the frame load.
func (p Pressure) Stalled() bool {
	return p.SomeAvg10 >= pressureSomeWarnPct || p.FullAvg10 >= pressureFullWarnPct
}

// Summary renders the numbers an operator reads at a glance.
func (p Pressure) Summary() string {
	return fmt.Sprintf("some %.1f%% / full %.1f%% (last 10s)", p.SomeAvg10, p.FullAvg10)
}

// ReadMemoryPressure returns the current memory PSI, preferring the cgroup's
// own file over the host's.
func ReadMemoryPressure() MemoryPressure { return readMemoryPressureFrom(psiPaths) }

func readMemoryPressureFrom(paths []string) MemoryPressure {
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue // absent, or unreadable in this sandbox: try the next
		}
		p, err := parsePressure(data)
		if err != nil {
			continue
		}
		return MemoryPressure{Available: true, Source: path, Pressure: p}
	}
	return MemoryPressure{}
}

// parsePressure reads the two-line PSI format:
//
//	some avg10=0.00 avg60=0.00 avg300=0.00 total=29
//	full avg10=0.00 avg60=0.00 avg300=0.00 total=9
//
// A file with neither line is an error rather than a zero value, so a kernel
// that changes the format is reported as unavailable instead of as "no
// pressure" - the failure mode that would quietly hide the thing being watched.
func parsePressure(data []byte) (Pressure, error) {
	var p Pressure
	var seen bool
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 4 {
			continue
		}
		kind := fields[0]
		if kind != "some" && kind != "full" {
			continue
		}
		for _, f := range fields[1:] {
			key, val, ok := strings.Cut(f, "=")
			if !ok {
				continue
			}
			n, err := strconv.ParseFloat(val, 64)
			if err != nil {
				continue // total= is an integer counter we do not use
			}
			switch {
			case kind == "some" && key == "avg10":
				p.SomeAvg10, seen = n, true
			case kind == "some" && key == "avg60":
				p.SomeAvg60 = n
			case kind == "some" && key == "avg300":
				p.SomeAvg300 = n
			case kind == "full" && key == "avg10":
				p.FullAvg10, seen = n, true
			case kind == "full" && key == "avg60":
				p.FullAvg60 = n
			case kind == "full" && key == "avg300":
				p.FullAvg300 = n
			}
		}
	}
	if err := sc.Err(); err != nil {
		return Pressure{}, err
	}
	if !seen {
		return Pressure{}, fmt.Errorf("no PSI averages found")
	}
	return p, nil
}
