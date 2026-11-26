// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"nimblegate/internal/engine"
)

// HistoryChecker runs only the secret frames over a partial tree: the file
// versions a push carries in its earlier commits but not in its final tree.
// The tip scan never sees those, yet relaying the push publishes them.
type HistoryChecker interface {
	CheckHistory(treeDir string) ([]engine.CheckResult, []engine.SuppressionLog, error)
}

// HistoryTooLargeID marks a push whose new history could not be scanned in the
// time budget. It rejects under enforcement: an unscanned secret must not be
// relayed, and pushing the history in parts always gets through.
const HistoryTooLargeID = "push/history-too-large"

// historyChunkBytes bounds one scanned chunk on disk; at most workers+1 chunks
// exist at once.
const historyChunkBytes int64 = 256 << 20

// DefaultHistoryWorkers is half the CPUs, between 1 and 4: other pushes and
// the dashboard share the box.
func DefaultHistoryWorkers() int {
	n := runtime.NumCPU() / 2
	if n < 1 {
		return 1
	}
	if n > 4 {
		return 4
	}
	return n
}

type historyBlob struct {
	commit string
	path   string // slash-separated, as git stores it
	blob   string
	size   int64
}

type historyChunk struct {
	blobs []historyBlob
	files map[string]bool
	dirs  map[string]bool
	bytes int64
}

// fits reports whether b can join the chunk: room left, and its path neither
// repeats nor collides with a file/directory already planned.
func (c *historyChunk) fits(b historyBlob, max int64) bool {
	if len(c.blobs) > 0 && c.bytes+b.size > max {
		return false
	}
	if c.files[b.path] || c.dirs[b.path] {
		return false
	}
	for d := path.Dir(b.path); d != "."; d = path.Dir(d) {
		if c.files[d] {
			return false
		}
	}
	return true
}

func (c *historyChunk) add(b historyBlob) {
	c.blobs = append(c.blobs, b)
	c.files[b.path] = true
	for d := path.Dir(b.path); d != "."; d = path.Dir(d) {
		c.dirs[d] = true
	}
	c.bytes += b.size
}

// planHistoryChunks packs blobs into chunks; versions of one path always land
// in different chunks, so every version is scanned at its real path.
func planHistoryChunks(blobs []historyBlob, max int64) []*historyChunk {
	var chunks []*historyChunk
	for _, b := range blobs {
		placed := false
		for _, c := range chunks {
			if c.fits(b, max) {
				c.add(b)
				placed = true
				break
			}
		}
		if !placed {
			c := &historyChunk{files: map[string]bool{}, dirs: map[string]bool{}}
			c.add(b)
			chunks = append(chunks, c)
		}
	}
	return chunks
}

// newHistoryBlobs lists the file versions that commits new to this repo add or
// change and that the final tree (newRev) does not hold. Commits already
// reachable from any ref - including the upstream history imported at
// registration - are excluded, so a routine push lists only its own commits.
func newHistoryBlobs(ctx context.Context, gitDir, newRev string) ([]historyBlob, error) {
	tip, err := tipBlobs(ctx, gitDir, newRev)
	if err != nil {
		return nil, err
	}
	revs := exec.CommandContext(ctx, "git", "--git-dir", gitDir, "rev-list", newRev, "--not", "--all")
	var revErr bytes.Buffer
	revs.Stderr = &revErr
	revOut, err := revs.Output()
	if err != nil {
		return nil, fmt.Errorf("rev-list: %w: %s", err, strings.TrimSpace(revErr.String()))
	}
	if len(bytes.TrimSpace(revOut)) == 0 {
		return nil, nil
	}
	dt := exec.CommandContext(ctx, "git", "--git-dir", gitDir, "diff-tree", "--stdin", "-r", "-m", "--root", "--no-renames", "--diff-filter=AM", "-z")
	dt.Stdin = bytes.NewReader(revOut)
	var dtErr bytes.Buffer
	dt.Stderr = &dtErr
	out, err := dt.Output()
	if err != nil {
		return nil, fmt.Errorf("diff-tree: %w: %s", err, strings.TrimSpace(dtErr.String()))
	}
	blobs := parseDiffTree(out, tip)
	if len(blobs) == 0 {
		return nil, nil
	}
	if err := fillBlobSizes(ctx, gitDir, blobs); err != nil {
		return nil, err
	}
	return blobs, nil
}

// parseDiffTree reads `diff-tree --stdin -r -z` raw output: a commit header
// token, then ":<mode> <mode> <old> <new> <status>" + path token pairs. Keeps
// regular files whose (path, blob) is new and not in the final tree.
func parseDiffTree(out []byte, tip map[string]string) []historyBlob {
	toks := strings.Split(string(out), "\x00")
	seen := map[string]bool{}
	var blobs []historyBlob
	commit := ""
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if t == "" {
			continue
		}
		if !strings.HasPrefix(t, ":") {
			commit, _, _ = strings.Cut(t, " ")
			continue
		}
		if i+1 >= len(toks) {
			break
		}
		p := toks[i+1]
		i++
		f := strings.Fields(t[1:])
		if len(f) < 5 {
			continue
		}
		mode, blob := f[1], f[3]
		if !strings.HasPrefix(mode, "100") { // symlinks (120000) and submodules (160000) carry no content
			continue
		}
		if tip[p] == blob || !filepath.IsLocal(filepath.FromSlash(p)) {
			continue
		}
		key := p + "\x00" + blob
		if seen[key] {
			continue
		}
		seen[key] = true
		blobs = append(blobs, historyBlob{commit: commit, path: p, blob: blob})
	}
	return blobs
}

func tipBlobs(ctx context.Context, gitDir, rev string) (map[string]string, error) {
	cmd := exec.CommandContext(ctx, "git", "--git-dir", gitDir, "ls-tree", "-r", "-z", rev)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ls-tree: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	tip := map[string]string{}
	for _, e := range strings.Split(string(out), "\x00") {
		meta, p, ok := strings.Cut(e, "\t")
		if !ok {
			continue
		}
		if f := strings.Fields(meta); len(f) == 3 {
			tip[p] = f[2]
		}
	}
	return tip, nil
}

func fillBlobSizes(ctx context.Context, gitDir string, blobs []historyBlob) error {
	var in bytes.Buffer
	for _, b := range blobs {
		in.WriteString(b.blob + "\n")
	}
	cmd := exec.CommandContext(ctx, "git", "--git-dir", gitDir, "cat-file", "--batch-check=%(objectsize)")
	cmd.Stdin = &in
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("cat-file sizes: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != len(blobs) {
		return fmt.Errorf("cat-file sizes: got %d answers for %d blobs", len(lines), len(blobs))
	}
	for i, l := range lines {
		n, err := strconv.ParseInt(strings.TrimSpace(l), 10, 64)
		if err != nil {
			return fmt.Errorf("cat-file sizes: %q", l)
		}
		blobs[i].size = n
	}
	return nil
}

// blobWriter streams blob contents out of one `git cat-file --batch` process.
type blobWriter struct {
	cmd *exec.Cmd
	in  io.WriteCloser
	out *bufio.Reader
}

func newBlobWriter(ctx context.Context, gitDir string) (*blobWriter, error) {
	cmd := exec.CommandContext(ctx, "git", "--git-dir", gitDir, "cat-file", "--batch")
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &blobWriter{cmd: cmd, in: in, out: bufio.NewReaderSize(out, 64<<10)}, nil
}

func (w *blobWriter) writeTo(blob, dest string) (err error) {
	if _, err := io.WriteString(w.in, blob+"\n"); err != nil {
		return err
	}
	header, err := w.out.ReadString('\n')
	if err != nil {
		return err
	}
	f := strings.Fields(header)
	if len(f) != 3 || f[1] != "blob" {
		return fmt.Errorf("cat-file: unexpected header %q", strings.TrimSpace(header))
	}
	size, err := strconv.ParseInt(f[2], 10, 64)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	if _, err := io.CopyN(file, w.out, size); err != nil {
		return err
	}
	_, err = w.out.Discard(1) // trailing newline after the content
	return err
}

func (w *blobWriter) close() {
	_ = w.in.Close()
	_ = w.cmd.Wait()
}

// scanHistory checks the new history of one ref with the secret frames. It
// returns results already relativized and annotated with the commit that holds
// each finding. A scan that does not finish by deadline yields a
// HistoryTooLargeID block rather than an error: the cure is pushing in parts.
func scanHistory(d PreReceiveDeps, hc HistoryChecker, r RefUpdate, deadline time.Time) ([]engine.CheckResult, []Suppression, error) {
	ctx := context.Background()
	if !deadline.IsZero() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, deadline)
		defer cancel()
	}
	blobs, err := newHistoryBlobs(ctx, d.GitDir, r.NewRev)
	if err != nil {
		if ctx.Err() != nil {
			return []engine.CheckResult{historyTooLarge()}, nil, nil
		}
		return nil, nil, err
	}
	if len(blobs) == 0 {
		return nil, nil, nil
	}
	chunks := planHistoryChunks(blobs, historyChunkBytes)
	workers := d.HistoryWorkers
	if workers <= 0 {
		workers = DefaultHistoryWorkers()
	}

	type staged struct {
		dir   string
		chunk *historyChunk
	}
	type scanned struct {
		res  []engine.CheckResult
		supp []Suppression
		err  error
	}
	jobs := make(chan staged)
	results := make(chan scanned, len(chunks))
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for s := range jobs {
				res, supp, err := checkHistoryChunk(ctx, d, hc, s.dir, s.chunk)
				removeStagedTree(s.dir)
				results <- scanned{res, supp, err}
			}
		}()
	}

	var stageErr error
	w, err := newBlobWriter(ctx, d.GitDir)
	if err != nil {
		stageErr = err
	} else {
	stage:
		for _, c := range chunks {
			dir, err := os.MkdirTemp(d.ScanTmpDir, "afgw-hist-")
			if err != nil {
				stageErr = err
				break
			}
			for _, b := range c.blobs {
				if err := w.writeTo(b.blob, filepath.Join(dir, filepath.FromSlash(b.path))); err != nil {
					removeStagedTree(dir)
					stageErr = err
					break stage
				}
			}
			select {
			case jobs <- staged{dir, c}:
			case <-ctx.Done():
				removeStagedTree(dir)
				break stage
			}
		}
		w.close()
	}
	close(jobs)
	wg.Wait()
	close(results)

	if ctx.Err() != nil {
		return []engine.CheckResult{historyTooLarge()}, nil, nil
	}
	if stageErr != nil {
		return nil, nil, fmt.Errorf("stage history: %w", stageErr)
	}
	var all []engine.CheckResult
	var supp []Suppression
	for s := range results {
		if s.err != nil {
			if errors.Is(s.err, context.DeadlineExceeded) {
				return []engine.CheckResult{historyTooLarge()}, nil, nil
			}
			return nil, nil, s.err
		}
		all = append(all, s.res...)
		supp = append(supp, s.supp...)
	}
	return mergeByFrame(all), supp, nil
}

// mergeByFrame folds the results of one frame across chunks into one line, so
// a fixture edited many times does not print one rejection line per version.
// The worst outcome wins; all hits are kept for the audit record.
func mergeByFrame(results []engine.CheckResult) []engine.CheckResult {
	sort.SliceStable(results, func(i, j int) bool { return results[i].FrameID < results[j].FrameID })
	var out []engine.CheckResult
	extra := map[int]int{}
	for _, r := range results {
		if n := len(out); n > 0 && out[n-1].FrameID == r.FrameID {
			last := &out[n-1]
			last.Hits = append(last.Hits, r.Hits...)
			if outcomeRank(r.Outcome) > outcomeRank(last.Outcome) {
				last.Outcome = r.Outcome
			}
			extra[n-1]++
			continue
		}
		out = append(out, r)
	}
	for i, n := range extra {
		out[i].Reason += fmt.Sprintf(" (+%d more earlier file version(s) in this push)", n)
	}
	return out
}

func outcomeRank(o engine.CheckOutcome) int {
	switch o {
	case engine.OutcomeBlock, engine.OutcomeError:
		return 3
	case engine.OutcomeWarn:
		return 2
	case engine.OutcomeInfo:
		return 1
	}
	return 0
}

// checkHistoryChunk runs the secret frames over one staged chunk under the
// gateway-held policy, bounded by ctx's deadline.
func checkHistoryChunk(ctx context.Context, d PreReceiveDeps, hc HistoryChecker, dir string, c *historyChunk) ([]engine.CheckResult, []Suppression, error) {
	if err := overlayPolicy(d.Policy.PolicyDir, dir); err != nil {
		return nil, nil, fmt.Errorf("overlay: %w", err)
	}
	type outcome struct {
		res  []engine.CheckResult
		supp []engine.SuppressionLog
		err  error
	}
	done := make(chan outcome, 1) // buffered: an abandoned run must not block forever
	go func() {
		res, supp, err := hc.CheckHistory(dir)
		done <- outcome{res, supp, err}
	}()
	var o outcome
	select {
	case o = <-done:
	case <-ctx.Done():
		return nil, nil, context.DeadlineExceeded
	}
	if o.err != nil {
		return nil, nil, o.err
	}
	res := relativizeResults(o.res, dir)
	commitOf := map[string]string{}
	for _, b := range c.blobs {
		commitOf[b.path] = b.commit
	}
	var kept []engine.CheckResult
	for _, r := range res {
		if r.Outcome == engine.OutcomePass || r.Outcome == "" {
			continue
		}
		r.Reason = annotateHistory(r, commitOf)
		kept = append(kept, r)
	}
	return kept, relativizeSuppressions(o.supp, dir), nil
}

// annotateHistory names the commits that hold the finding and what to do: the
// file is gone from the final tree, so only rewriting history removes it.
func annotateHistory(r engine.CheckResult, commitOf map[string]string) string {
	seen := map[string]bool{}
	var shas []string
	for _, h := range r.Hits {
		if c := commitOf[filepath.ToSlash(h.File)]; c != "" && !seen[c] {
			seen[c] = true
			shas = append(shas, shortSHA(c))
		}
	}
	where := "an earlier commit of this push"
	if len(shas) > 0 {
		where = "commit " + strings.Join(shas, ", ")
	}
	return fmt.Sprintf("%s (in %s, not in the final tree: remove it from history with a rebase or squash, then push again)", r.Reason, where)
}

func shortSHA(s string) string {
	if len(s) > 10 {
		return s[:10]
	}
	return s
}

func historyTooLarge() engine.CheckResult {
	return engine.CheckResult{
		FrameID: HistoryTooLargeID,
		Outcome: engine.OutcomeBlock,
		Reason:  "the new history in this push is too large to scan for secrets in one go - push it in parts, older commits first (git push <remote> <older-commit>:<branch>)",
	}
}
