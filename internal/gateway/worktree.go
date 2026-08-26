// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"nimblegate/internal/scanignore"
)

// errTreeTooLarge is returned when a push expands past the configured cap.
var errTreeTooLarge = errors.New("pushed tree exceeds the configured size cap")

// cappedReader fails the stream once more than max bytes have passed through.
// The cap is applied to the tar stream rather than to the pack, because the two
// are unrelated: receive.maxInputSize bounds a compressed pack, and a file of
// 10 GB of zeros is a tiny object that still extracts in full.
// onExceed must stop the writer feeding r. Failing the read alone is not
// enough: exec closes the consumer's stdin, and `git archive` then blocks
// forever writing into a pipe nobody drains, hanging the whole hook.
type cappedReader struct {
	r        io.Reader
	max      int64
	seen     int64
	onExceed func()
	fired    bool
}

func (c *cappedReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.seen += int64(n)
	if c.seen > c.max {
		if !c.fired {
			c.fired = true
			if c.onExceed != nil {
				c.onExceed()
			}
		}
		return n, fmt.Errorf("%w (%d bytes)", errTreeTooLarge, c.max)
	}
	return n, err
}

// exceeded reports whether r tripped its cap, for callers turning a downstream
// "the pipe closed early" error back into the real reason.
func exceeded(r io.Reader) bool {
	c, ok := r.(*cappedReader)
	return ok && c.seen > c.max
}

// materializeTree exports the tree at rev from the bare repo gitDir into destDir
// using `git archive | tar -x`. destDir must already exist. maxBytes caps the
// extracted stream (0 = unlimited).
func materializeTree(gitDir, rev, destDir string, maxBytes int64) error {
	archive := exec.Command("git", "--git-dir", gitDir, "archive", "--format=tar", rev)
	var aerr bytes.Buffer
	archive.Stderr = &aerr
	untar := exec.Command("tar", "-x", "-C", destDir)
	var terr bytes.Buffer
	untar.Stderr = &terr

	archiveOut, err := archive.StdoutPipe()
	if err != nil {
		return err
	}
	untarIn, err := untar.StdinPipe()
	if err != nil {
		return err
	}
	if err := untar.Start(); err != nil {
		return err
	}
	if err := archive.Start(); err != nil {
		_ = untarIn.Close()
		_ = untar.Wait()
		return err
	}

	// The copy is explicit rather than handed to exec as cmd.Stdin for two
	// reasons: the cap has to be able to kill `git archive` (closing tar's end
	// alone leaves archive blocked forever on a pipe nobody drains), and Wait
	// may only run once every read has finished, which exec's own copier
	// goroutine cannot promise.
	var src io.Reader = archiveOut
	if maxBytes > 0 {
		src = &cappedReader{r: archiveOut, max: maxBytes, onExceed: func() {
			if archive.Process != nil {
				_ = archive.Process.Kill()
			}
		}}
	}
	_, copyErr := io.Copy(untarIn, src)
	_ = untarIn.Close() // EOF for tar, whatever happened above
	archiveErr := archive.Wait()
	untarErr := untar.Wait()

	// A broken pipe is a symptom of whichever child died, so the children's own
	// errors are reported first - they carry the reason.
	switch {
	case exceeded(src):
		return fmt.Errorf("%w (%d bytes)", errTreeTooLarge, maxBytes)
	case archiveErr != nil:
		// tar's stderr is folded in too: when tar dies first (full temp
		// filesystem, unwritable dest), archive only ever sees the resulting
		// EPIPE, so tar holds the real cause.
		return fmt.Errorf("git archive %s: %w: %s%s", rev, archiveErr, strings.TrimSpace(aerr.String()), tarDetail(terr))
	case untarErr != nil:
		return fmt.Errorf("untar: %w%s", untarErr, tarDetail(terr))
	case copyErr != nil:
		return fmt.Errorf("stream: %w", copyErr)
	}
	return neutralizeEscapingSymlinks(destDir)
}

// neutralizeEscapingSymlinks replaces every symlink under dir whose target
// resolves outside dir with an empty regular file.
//
// A pushed tree is attacker-controlled, and the scan reads what it finds: a
// pusher who commits "cred -> /etc/nimblegate-gateway/repos/x/credential" and
// pushes it gets the gate to open that file and report findings about it -
// an arbitrary-read oracle against anything the gateway user can read,
// including other repos and the gateway's own upstream credential. Extraction
// itself cannot escape (a git tree holds no ".." entry, and cannot carry both
// a symlink "x" and an entry under "x/"), so reads are the whole exposure.
//
// Replaced rather than deleted: the path stays visible, so a frame keyed on
// filenames still sees a pushed ".env" that was smuggled in as a symlink.
// Links that stay inside the tree are ordinary content and are left alone.
func neutralizeEscapingSymlinks(dir string) error {
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		root = dir // dir itself is ours and was just created; fall back to it
	}
	// Collect first, act second. Resolving every link against the untouched
	// tree keeps the result independent of walk order: neutralising as we go
	// would leave "a -> b -> /outside" as a live link to an already-emptied
	// "b" whenever b sorts first - safe, but only by accident.
	var escaping []string
	if err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.Type()&fs.ModeSymlink == 0 {
			return nil // WalkDir is Lstat-based, so it never descends INTO a symlink
		}
		if !within(root, resolveLink(p)) {
			escaping = append(escaping, p)
		}
		return nil
	}); err != nil {
		return err
	}
	for _, p := range escaping {
		if err := os.Remove(p); err != nil {
			return err
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// resolveLink returns the real path a symlink points at, following the whole
// chain. A dangling or unresolvable link falls back to lexical resolution, so
// a broken link inside the tree stays inside (reading it simply fails) while
// anything aimed outward is still caught.
func resolveLink(p string) string {
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real
	}
	target, err := os.Readlink(p)
	if err != nil {
		return "" // unreadable link: treat as escaping
	}
	if filepath.IsAbs(target) {
		return filepath.Clean(target)
	}
	return filepath.Clean(filepath.Join(filepath.Dir(p), target))
}

// within reports whether path sits inside root.
func within(root, path string) bool {
	if path == "" {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// tarDetail renders the useful part of tar's stderr for an error message.
// Without it a full temp filesystem reports "exit status 2" and nothing else,
// leaving the operator no way to tell ENOSPC from an unwritable dir or a
// corrupt object. Two lines rather than one: on a full filesystem tar's first
// line is a short-write byte count and the plain "No space left on device"
// only appears on the next. Repeats (tar prints the same reason once per file)
// and its generic trailers are dropped.
func tarDetail(buf bytes.Buffer) string {
	var picked []string
	seen := map[string]bool{}
	for _, line := range strings.Split(buf.String(), "\n") {
		s := strings.TrimSpace(line)
		if s == "" || strings.Contains(s, "Exiting with failure status") || strings.Contains(s, "Error is not recoverable") {
			continue
		}
		reason := s
		if i := strings.LastIndex(s, ": "); i >= 0 {
			reason = s[i+2:]
		}
		if seen[reason] {
			continue
		}
		seen[reason] = true
		picked = append(picked, s)
		if len(picked) == 2 {
			break
		}
	}
	if len(picked) == 0 {
		return ""
	}
	return ": " + strings.Join(picked, "; ")
}

// overlayPolicy copies the gateway-held appframes.toml (and a .appframes/ dir if
// present) from policyDir onto destDir, overwriting any pushed config. This is
// what makes the enforced policy gateway-held: the pushed tree's own config is
// replaced before the check runs.
func overlayPolicy(policyDir, destDir string) error {
	// Remove config the push brought in, so the enforced policy is purely
	// gateway-held - a push cannot shadow/downgrade frames via its own config.
	_ = os.Remove(filepath.Join(destDir, "appframes.toml"))
	_ = os.RemoveAll(filepath.Join(destDir, ".appframes"))

	// Strip any pushed .appframes-ignore markers anywhere in the tree - they are
	// a scan-policy surface the push must NOT control (same reason we wipe
	// appframes.toml). The engine discovers these markers tree-wide; leaving one
	// in place would let a push exclude files (e.g. "*.pem") from the gate.
	_ = filepath.WalkDir(destDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // best-effort cleanup
		}
		if !d.IsDir() && d.Name() == scanignore.MarkerFilename {
			_ = os.Remove(p)
		}
		return nil
	})

	src := filepath.Join(policyDir, "appframes.toml")
	if b, err := os.ReadFile(src); err == nil {
		if err := os.WriteFile(filepath.Join(destDir, "appframes.toml"), b, 0o644); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if fi, err := os.Stat(filepath.Join(policyDir, ".appframes")); err == nil && fi.IsDir() {
		return copyDir(filepath.Join(policyDir, ".appframes"), filepath.Join(destDir, ".appframes"))
	}
	return nil
}

// copyDir recursively copies src to dst (files 0644, dirs 0755).
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
}
