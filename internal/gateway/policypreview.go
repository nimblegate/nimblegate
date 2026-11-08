// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"

	"nimblegate/internal/engine"
)

// PreviewInput is one "what would the next push do under this frame list"
// question. Policy, GitDir, Checker, ScanTmpDir, MaxTreeBytes and ScanTimeout
// mean exactly what they mean in PreReceiveDeps; Enabled is the candidate
// [frames] enabled list evaluated in place of the stored one.
type PreviewInput struct {
	Policy       Policy
	GitDir       string
	Checker      Checker
	Enabled      []string
	ScanTmpDir   string
	MaxTreeBytes int64
	ScanTimeout  time.Duration
}

// PreviewFinding is one row of a preview: a single hit, or the whole result
// when a frame reports no per-file hits.
type PreviewFinding struct {
	Severity string // BLOCK | ERROR | WARN | INFO
	Frame    string
	File     string // relative to the tree root; "" when the result names no file
	Line     int
	Label    string
	Origin   string
}

// previewRowCap bounds the rows kept per severity group. The counts stay exact.
const previewRowCap = 200

// PreviewResult is the gate's answer for the latest pushed code of a repo.
type PreviewResult struct {
	Ref, Rev   string
	NoCode     bool   // the bare repo has no commits yet
	NoGatedRef bool   // commits exist, but none on a ref the gate checks
	Disabled   bool   // gating is off: the next push relays unchecked
	ScanError  string // the gate could not evaluate the tree (rejects under enforcement)
	BlockCount int
	WarnCount  int
	Blocks     []PreviewFinding
	Warns      []PreviewFinding
}

// WouldReject reports whether the gate would reject the next push under
// enforcement.
func (r PreviewResult) WouldReject() bool { return r.BlockCount > 0 || r.ScanError != "" }

var (
	previewLocks sync.Map // gitDir -> *sync.Mutex
	previewCache = struct {
		sync.Mutex
		order []string
		m     map[string]PreviewResult
	}{m: map[string]PreviewResult{}}
)

const previewCacheCap = 64

// PreviewPolicy answers what the gate would do with the latest pushed code of
// the repo if its frame list were in.Enabled. It runs the gate's own steps for
// one ref - materializeTree, overlayPolicy, the injected Checker under the
// same deadline, Decide - with only the overlaid [frames] enabled list
// replaced. Results are cached per (rev, candidate list, policy files), and
// one preview per repo runs at a time.
func PreviewPolicy(in PreviewInput) PreviewResult {
	if !in.Policy.Enabled {
		return PreviewResult{Disabled: true}
	}
	lockAny, _ := previewLocks.LoadOrStore(in.GitDir, &sync.Mutex{})
	mu := lockAny.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()

	ref, rev, anyCommit := previewRef(in.GitDir, in.Policy)
	if rev == "" {
		if anyCommit {
			return PreviewResult{NoGatedRef: true}
		}
		return PreviewResult{NoCode: true}
	}
	key, err := previewKey(in, ref, rev)
	if err == nil {
		previewCache.Lock()
		res, ok := previewCache.m[key]
		previewCache.Unlock()
		if ok {
			return res
		}
	}
	res := runPreview(in, ref, rev)
	if err == nil {
		previewCache.Lock()
		if _, ok := previewCache.m[key]; !ok {
			previewCache.order = append(previewCache.order, key)
			if len(previewCache.order) > previewCacheCap {
				delete(previewCache.m, previewCache.order[0])
				previewCache.order = previewCache.order[1:]
			}
		}
		previewCache.m[key] = res
		previewCache.Unlock()
	}
	return res
}

func runPreview(in PreviewInput, ref, rev string) PreviewResult {
	res := PreviewResult{Ref: ref, Rev: rev}
	results := previewScan(in, rev)
	dec := Decide(in.Policy, []RefUpdate{{OldRev: zeroRev, NewRev: rev, Name: ref}}, map[string][]engine.CheckResult{ref: results})
	if dec.ScanFailed {
		res.ScanError = strings.Join(dec.ScanFailures, "; ")
		return res
	}
	for _, r := range results {
		var blocking bool
		switch r.Outcome {
		case engine.OutcomeBlock, engine.OutcomeError:
			blocking = true
		case engine.OutcomeWarn, engine.OutcomeInfo:
		default:
			continue
		}
		rows := previewRows(r)
		if blocking {
			res.BlockCount += len(rows)
			res.Blocks = appendCapped(res.Blocks, rows)
		} else {
			res.WarnCount += len(rows)
			res.Warns = appendCapped(res.Warns, rows)
		}
	}
	return res
}

// previewScan is RunPreReceive's per-ref staging and check, with failures
// turned into the same synthetic ScanFailedID result the hook produces.
func previewScan(in PreviewInput, rev string) []engine.CheckResult {
	failed := func(reason string) []engine.CheckResult {
		return []engine.CheckResult{{FrameID: ScanFailedID, Outcome: engine.OutcomeError, Reason: reason}}
	}
	tmp, err := os.MkdirTemp(in.ScanTmpDir, "nimblegate-preview-")
	if err != nil {
		return failed(err.Error())
	}
	defer os.RemoveAll(tmp)
	if err := materializeTree(in.GitDir, rev, tmp, in.MaxTreeBytes); err != nil {
		return failed("materialize: " + err.Error())
	}
	if err := overlayPolicyEnabled(in.Policy.PolicyDir, tmp, in.Enabled); err != nil {
		return failed("overlay: " + err.Error())
	}
	res, _, err := checkWithDeadline(in.Checker, tmp, in.ScanTimeout)
	if err != nil {
		return failed("check: " + err.Error())
	}
	return relativizeResults(res, tmp)
}

func previewRows(r engine.CheckResult) []PreviewFinding {
	sev := severityWord(r.Outcome)
	if len(r.Hits) == 0 {
		return []PreviewFinding{{Severity: sev, Frame: r.FrameID, Label: findingMessage(r), Origin: r.Origin}}
	}
	out := make([]PreviewFinding, 0, len(r.Hits))
	for _, h := range r.Hits {
		out = append(out, PreviewFinding{Severity: sev, Frame: r.FrameID, File: h.File, Line: h.Line, Label: boundMessage(h.Label), Origin: r.Origin})
	}
	return out
}

func appendCapped(dst, rows []PreviewFinding) []PreviewFinding {
	room := previewRowCap - len(dst)
	if room <= 0 {
		return dst
	}
	if len(rows) > room {
		rows = rows[:room]
	}
	return append(dst, rows...)
}

// overlayPolicyEnabled is overlayPolicy with the overlaid [frames] enabled list
// replaced by enabled. Only the copy inside destDir changes.
func overlayPolicyEnabled(policyDir, destDir string, enabled []string) error {
	if err := overlayPolicy(policyDir, destDir); err != nil {
		return err
	}
	p := filepath.Join(destDir, "appframes.toml")
	doc := map[string]any{}
	b, err := os.ReadFile(p)
	switch {
	case err == nil:
		if _, err := toml.Decode(string(b), &doc); err != nil {
			return err
		}
	case os.IsNotExist(err):
		if len(enabled) == 0 {
			return nil // absent config and an empty list both mean every frame
		}
	default:
		return err
	}
	frames, _ := doc["frames"].(map[string]any)
	if frames == nil {
		frames = map[string]any{}
	}
	list := append([]string{}, enabled...)
	frames["enabled"] = list
	doc["frames"] = frames
	var buf bytes.Buffer
	if err := toml.NewEncoder(&buf).Encode(doc); err != nil {
		return err
	}
	return os.WriteFile(p, buf.Bytes(), 0o644)
}

// previewRef picks the ref whose code the next push most likely builds on:
// the default branch (HEAD's target) when the gate checks it, otherwise the
// most recently committed gated ref. anyCommit reports whether the repo holds
// any commit at all.
func previewRef(gitDir string, p Policy) (ref, rev string, anyCommit bool) {
	git := func(args ...string) string {
		out, err := exec.Command("git", append([]string{"--git-dir", gitDir}, args...)...).Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	}
	if head := git("symbolic-ref", "-q", "HEAD"); head != "" {
		if r := git("rev-parse", "--verify", "-q", head+"^{commit}"); r != "" {
			anyCommit = true
			if isGatedRef(p, head) {
				return head, r, true
			}
		}
	}
	for _, line := range strings.Split(git("for-each-ref", "--sort=-committerdate", "--format=%(objecttype) %(objectname) %(refname)"), "\n") {
		f := strings.Fields(line)
		if len(f) != 3 || f[0] != "commit" {
			continue
		}
		anyCommit = true
		if isGatedRef(p, f[2]) {
			return f[2], f[1], true
		}
	}
	return "", "", anyCommit
}

// previewKey hashes everything that can change the answer: the commit, the
// candidate list, the gateway-held policy files the overlay copies (config,
// scan settings, linters, whitelist), the resource caps and the UTC date
// (whitelist entries expire by day).
func previewKey(in PreviewInput, ref, rev string) (string, error) {
	h := sha256.New()
	cands := append([]string{}, in.Enabled...)
	sort.Strings(cands)
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s\x00%d\x00%s\x00%s\x00", in.GitDir, in.Policy.PolicyDir, ref, rev, in.MaxTreeBytes, in.ScanTimeout, time.Now().UTC().Format("2006-01-02"))
	fmt.Fprintf(h, "%q\x00", cands)
	hashFile := func(p string) error {
		b, err := os.ReadFile(p)
		if os.IsNotExist(err) {
			fmt.Fprintf(h, "absent:%s\x00", p)
			return nil
		}
		if err != nil {
			return err
		}
		fmt.Fprintf(h, "%s\x00%d\x00", p, len(b))
		h.Write(b)
		return nil
	}
	if err := hashFile(filepath.Join(in.Policy.PolicyDir, "appframes.toml")); err != nil {
		return "", err
	}
	err := filepath.WalkDir(filepath.Join(in.Policy.PolicyDir, ".appframes"), func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		return hashFile(p)
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
