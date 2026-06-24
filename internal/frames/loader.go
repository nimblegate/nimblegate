// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package frames

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// LoadFromDir walks dir recursively and returns every successfully-parsed
// frame markdown file found, plus a slice of per-file errors for frames that
// failed to parse. A single bad frame no longer drops the rest of the project's
// frames; callers can choose to surface partial-load warnings while still
// running the frames that were valid.
//
// Files inside underscore-prefixed subdirs (`_canonical/`, `_incidents/`)
// are skipped - those are nimblegate-managed metadata, not user-authored
// frames. Non-`.md` files are skipped. Files starting with `.` (dotfiles)
// are also skipped - `.draft.md` is not a frame.
//
// If dir does not exist, returns (nil, nil) - absence of project frames is
// not an error. If dir exists but is not a directory, returns one error.
// Other walker errors (e.g. permission denied on a subdir) appear as entries
// in the errors slice; the walk continues past them where possible.
func LoadFromDir(dir string) ([]Frame, []error) {
	info, err := os.Stat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, []error{fmt.Errorf("LoadFromDir: stat %s: %w", dir, err)}
	}
	if !info.IsDir() {
		return nil, []error{fmt.Errorf("LoadFromDir: %s is not a directory", dir)}
	}

	var out []Frame
	var errs []error
	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			errs = append(errs, fmt.Errorf("walk %s: %w", path, walkErr))
			// Continue past walker errors on individual files; abort only on root.
			if path == dir {
				return walkErr
			}
			return nil
		}
		if d.IsDir() {
			if d.Name() == "_canonical" {
				return filepath.SkipDir
			}
			// Underscore-prefixed dirs are nimblegate-managed metadata, not
			// user-authored frames (_canonical/, _incidents/).
			if path != dir && strings.HasPrefix(d.Name(), "_") {
				return filepath.SkipDir
			}
			// Skip hidden subdirs (e.g. .git, .hidden).
			if path != dir && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		// Only consider .md files.
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		// Skip hidden files (e.g. .draft.md).
		if strings.HasPrefix(d.Name(), ".") {
			return nil
		}
		// Skip symlinks. A frame .md should be a real file inside the
		// .appframes/ tree. A symlink might point anywhere on disk
		// (including outside the project) - refuse to follow it and report
		// so the user notices.
		info, statErr := d.Info()
		if statErr != nil {
			errs = append(errs, fmt.Errorf("stat %s: %w", path, statErr))
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			errs = append(errs, fmt.Errorf("%s: symlink frames are not followed (point to %s); replace with a real file", path, readlinkOrUnknown(path)))
			return nil
		}
		f, openErr := os.Open(path)
		if openErr != nil {
			errs = append(errs, openErr)
			return nil
		}
		defer f.Close()
		frame, parseErr := Parse(f, path)
		if parseErr != nil {
			errs = append(errs, parseErr)
			return nil
		}
		out = append(out, frame)
		return nil
	})
	if walkErr != nil && len(errs) == 0 {
		errs = append(errs, walkErr)
	}

	// Detect duplicate IDs within the loaded set. Two project frames with the
	// same category/name silently overwrite each other in the registry; surface
	// this here so the user finds out at load time, not when the wrong one
	// happens to win.
	out, dupErrs := dedupeAndReport(out)
	errs = append(errs, dupErrs...)

	return out, errs
}

// readlinkOrUnknown returns the symlink target or a placeholder if the
// target can't be read (broken or permission denied).
func readlinkOrUnknown(path string) string {
	target, err := os.Readlink(path)
	if err != nil {
		return "?"
	}
	return target
}

// dedupeAndReport returns a slice with at most one frame per ID, plus one
// error per detected collision. The first frame seen for each ID is kept;
// subsequent collisions are reported but otherwise discarded.
func dedupeAndReport(in []Frame) ([]Frame, []error) {
	seen := map[string]Frame{}
	var out []Frame
	var errs []error
	for _, f := range in {
		id := f.ID()
		if first, dup := seen[id]; dup {
			errs = append(errs, fmt.Errorf(
				"duplicate frame id %q: %s and %s - first wins, second is ignored",
				id, first.SourcePath, f.SourcePath))
			continue
		}
		seen[id] = f
		out = append(out, f)
	}
	return out, errs
}
