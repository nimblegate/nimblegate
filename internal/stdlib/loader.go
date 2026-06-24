// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package stdlib

import (
	"fmt"
	"io/fs"
	"strings"

	"nimblegate/internal/frames"
)

// TestdataFS returns a subtree of the embedded testdata for one frame.
// The path under testdata/ matches the frame ID's "<category>/<name>".
// Returns the testdata-rooted FS (positives/ + negatives/ at the root)
// and ok=true when testdata exists, or ok=false when this frame has no
// corpus yet (still "pre-architecture" or "pending" grade).
//
// Added 2026-05-20 with Phase 1 Slice 2.
func TestdataFS(frameID string) (fs.FS, bool) {
	sub, err := fs.Sub(testdataFS, "testdata/"+frameID)
	if err != nil {
		return nil, false
	}
	// Probe for existence - fs.Sub doesn't error on missing dirs.
	if _, err := fs.ReadDir(sub, "."); err != nil {
		return nil, false
	}
	return sub, true
}

// Load reads every frame markdown file embedded under frames/ EXCEPT
// frames/patterns/ and returns the parsed list. Patterns are loaded via
// LoadPatterns. Errors include the source path so the caller can
// identify which frame failed to parse.
func Load() ([]frames.Frame, error) {
	subFS, err := fs.Sub(frameFS, "frames")
	if err != nil {
		return nil, fmt.Errorf("stdlib: unable to open embed subtree: %w", err)
	}

	var out []frames.Frame
	err = fs.WalkDir(subFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// Patterns live in frames/patterns/ and are loaded by
			// LoadPatterns - skip the whole subtree here so we don't
			// try to parse them as Frame markdown (different schema).
			if path == "patterns" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		f, err := subFS.Open(path)
		if err != nil {
			return fmt.Errorf("stdlib: open %s: %w", path, err)
		}
		defer f.Close()
		frame, err := frames.Parse(f, "stdlib:"+path)
		if err != nil {
			return fmt.Errorf("stdlib: parse %s: %w", path, err)
		}
		out = append(out, frame)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// LoadPatterns reads every pattern markdown file under frames/patterns/
// and returns the parsed list. Patterns describe the structural shape
// of mistakes; frames are platform-specific instances of patterns.
//
// Added 2026-05-20 with the Phase 1 architecture. Returns (nil, nil)
// when the patterns subdir is absent so older builds without the
// patterns layout continue to work.
func LoadPatterns() ([]frames.Pattern, error) {
	subFS, err := fs.Sub(frameFS, "frames/patterns")
	if err != nil {
		return nil, nil
	}

	var out []frames.Pattern
	err = fs.WalkDir(subFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// Missing patterns dir is not an error - older builds.
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		f, err := subFS.Open(path)
		if err != nil {
			return fmt.Errorf("stdlib: open pattern %s: %w", path, err)
		}
		defer f.Close()
		p, err := frames.ParsePattern(f, "stdlib:patterns/"+path)
		if err != nil {
			return fmt.Errorf("stdlib: parse pattern %s: %w", path, err)
		}
		out = append(out, p)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
