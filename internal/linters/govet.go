// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package linters

import (
	"encoding/json"
	"io"
	"io/fs"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"nimblegate/internal/config"
	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
)

// goVetFrameID is the synthetic frame ID every go vet finding carries, so
// it slots into category-ordered output and whitelist matching exactly
// like a native frame.
const goVetFrameID = "app-correctness/go-vet"

// GoVet is the `go vet` adapter.
type GoVet struct{}

// ID is the synthetic frame ID go vet findings carry.
func (GoVet) ID() string { return goVetFrameID }

// Available reports whether the `go` toolchain is on PATH.
func (GoVet) Available() bool {
	_, err := exec.LookPath("go")
	return err == nil
}

// Run discovers every go.mod under projectRoot (honoring excludedDirs),
// runs `go vet -json ./...` in each, and returns one merged CheckResult.
// A machine without the Go toolchain gets a single SKIP - a missing tool
// must never fail the gate.
func (g GoVet) Run(projectRoot string, cfg config.LinterConfig, excludedDirs []string) engine.CheckResult {
	if !g.Available() {
		return skipResult(goVetFrameID, "go vet: skipped (go toolchain not found on PATH)")
	}
	outcome := resolveOutcome(cfg.Severity)
	var runs []goVetModuleRun
	for _, dir := range discoverGoModDirs(linterRoot(projectRoot, cfg), excludedDirs) {
		cmd := exec.Command("go", "vet", "-json", "./...")
		cmd.Dir = dir
		// go vet writes its report to stderr and exits non-zero when it finds
		// issues; CombinedOutput captures both streams and the non-zero exit is
		// expected, not an error to propagate. Paths are resolved relative to
		// projectRoot (not dir) so findings read project-relative everywhere.
		out, runErr := cmd.CombinedOutput()
		res := finalizeRun(parseGoVetOutput(string(out), projectRoot, outcome), runErr != nil)
		runs = append(runs, goVetModuleRun{Dir: relTo(dir, projectRoot), Result: res})
	}
	r := aggregateGoVet(runs, outcome, cfg.Disable)
	r.Timestamp = time.Now().UTC()
	return r
}

// goVetModuleRun pairs a discovered module dir (project-relative) with the
// CheckResult parsed from running go vet inside it.
type goVetModuleRun struct {
	Dir    string
	Result engine.CheckResult
}

// discoverGoModDirs walks root and returns every directory containing a
// go.mod, in lexical order. It prunes .git and any directory whose name is
// in excludedDirs (the project's [scan] excludes) so vendored / archived /
// node_modules go.mod files don't get vetted. Nested modules are kept -
// `go vet ./...` doesn't cross a module boundary, so each go.mod dir is its
// own vet target.
func discoverGoModDirs(root string, excludedDirs []string) []string {
	excluded := map[string]bool{".git": true}
	for _, d := range excludedDirs {
		excluded[d] = true
	}
	var dirs []string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable subtrees rather than abort the whole walk
		}
		if d.IsDir() {
			if path != root && excluded[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == "go.mod" {
			dirs = append(dirs, filepath.Dir(path))
		}
		return nil
	})
	sort.Strings(dirs)
	return dirs
}

// aggregateGoVet merges per-module results into one go vet CheckResult.
// Hits anywhere → findingOutcome with all hits sorted into one Reason.
// No hits but a module couldn't build → WARN naming the module(s). All
// clean → PASS. No modules discovered → SKIP (nothing to vet, not a pass).
func aggregateGoVet(runs []goVetModuleRun, findingOutcome engine.CheckOutcome, disable []string) engine.CheckResult {
	res := engine.CheckResult{FrameID: goVetFrameID, Category: frames.CategoryAppCorrectness}
	if len(runs) == 0 {
		res.Outcome = engine.OutcomeSkip
		res.Reason = "go vet: skipped (no Go modules found under project)"
		return res
	}

	var hits []engine.Hit
	var warnDirs []string
	for _, r := range runs {
		hits = append(hits, r.Result.Hits...)
		if r.Result.Outcome == engine.OutcomeWarn && len(r.Result.Hits) == 0 {
			warnDirs = append(warnDirs, r.Dir)
		}
	}

	// Merge + drop disabled analyzers via the shared builder. If all hits are
	// disabled this becomes a PASS, so check warnDirs only when no hits remain.
	if merged := buildResult(goVetFrameID, "go vet", hits, findingOutcome, disable); len(merged.Hits) > 0 {
		return merged
	}

	if len(warnDirs) > 0 {
		res.Outcome = engine.OutcomeWarn
		res.Reason = "go vet: could not analyze " + strconv.Itoa(len(warnDirs)) +
			" module(s): " + strings.Join(warnDirs, ", ") + " (build error? run `go vet ./...` there)"
		return res
	}

	res.Outcome = engine.OutcomePass
	res.Reason = "go vet: no findings"
	return res
}

// goVetDiag is one diagnostic in go vet's JSON output.
type goVetDiag struct {
	Posn    string `json:"posn"`
	Message string `json:"message"`
}

// parseGoVetOutput turns the combined output of `go vet -json ./...` into a
// single CheckResult. projectRoot makes finding paths project-relative for
// clean display + whitelist matching; findingOutcome is the severity to
// stamp when there are findings (a no-finding run is always PASS).
//
// The stream is a sequence of concatenated pretty-printed JSON objects -
// one per package, shaped {pkgpath: {analyzer: [{posn, message}]}} -
// interleaved with `#`-prefixed progress lines that are not JSON.
func parseGoVetOutput(out, projectRoot string, findingOutcome engine.CheckOutcome) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  goVetFrameID,
		Category: frames.CategoryAppCorrectness,
	}

	var buf strings.Builder
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "#") {
			continue
		}
		buf.WriteString(line)
		buf.WriteByte('\n')
	}

	dec := json.NewDecoder(strings.NewReader(buf.String()))
	var hits []engine.Hit
	for {
		var pkg map[string]map[string][]goVetDiag
		err := dec.Decode(&pkg)
		if err == io.EOF {
			break
		}
		if err != nil {
			// Malformed object - report what parsed so far rather than
			// discarding real findings on a trailing oddity.
			break
		}
		for _, analyzers := range pkg {
			for analyzer, diags := range analyzers {
				for _, d := range diags {
					file, line := splitPosn(d.Posn)
					hits = append(hits, engine.Hit{
						File:  relTo(file, projectRoot),
						Line:  line,
						Label: analyzer + ": " + d.Message,
					})
				}
			}
		}
	}

	if len(hits) == 0 {
		res.Outcome = engine.OutcomePass
		res.Reason = "go vet: no findings"
		return res
	}

	// go vet decodes into maps (random iteration); sort so Hits, Reason,
	// and the rebuilt-after-suppression Reason are all deterministic.
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].File != hits[j].File {
			return hits[i].File < hits[j].File
		}
		if hits[i].Line != hits[j].Line {
			return hits[i].Line < hits[j].Line
		}
		return hits[i].Label < hits[j].Label
	})

	parts := make([]string, len(hits))
	for i, h := range hits {
		parts[i] = h.Format()
	}
	res.Outcome = findingOutcome
	res.Hits = hits
	res.Reason = "go vet: " + strings.Join(parts, "; ")
	return res
}

// finalizeRun adjusts a parsed result for go vet's exit status. go vet exits
// non-zero both when it finds issues (then Hits is populated - leave as-is)
// and when it cannot build the packages (then nothing parsed - a "no
// findings" PASS would be a lie, so surface a WARN that points at the cause).
func finalizeRun(res engine.CheckResult, exitedNonZero bool) engine.CheckResult {
	if exitedNonZero && len(res.Hits) == 0 {
		res.Outcome = engine.OutcomeWarn
		res.Reason = "go vet: could not analyze (non-zero exit, no findings parsed - likely a build error; run `go vet ./...`)"
	}
	return res
}

// splitPosn parses a go vet "file:line:col" position. It reads line/col from
// the right so a path containing ':' (rare on POSIX) doesn't break file
// extraction. Returns line 0 when the position has no parseable line.
func splitPosn(posn string) (string, int) {
	last := strings.LastIndex(posn, ":")
	if last < 0 {
		return posn, 0
	}
	rest := posn[:last]
	secondLast := strings.LastIndex(rest, ":")
	if secondLast < 0 {
		// "file:line" with no column.
		if n, err := strconv.Atoi(posn[last+1:]); err == nil {
			return rest, n
		}
		return posn, 0
	}
	n, err := strconv.Atoi(rest[secondLast+1:])
	if err != nil {
		return rest[:secondLast], 0
	}
	return rest[:secondLast], n
}

// relTo makes an absolute finding path project-relative for clean display
// and whitelist matching. Paths that aren't under projectRoot are kept
// absolute rather than turned into escaping ../.. paths.
func relTo(file, projectRoot string) string {
	if projectRoot == "" || !filepath.IsAbs(file) {
		return file
	}
	if !strings.HasPrefix(file, projectRoot+string(filepath.Separator)) {
		return file
	}
	rel, err := filepath.Rel(projectRoot, file)
	if err != nil {
		return file
	}
	return rel
}
