// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package engine

import (
	"fmt"
	"io/fs"
	"strings"

	"nimblegate/internal/buckets"
	v2 "nimblegate/internal/config/v2"
	"nimblegate/internal/frames"
	"nimblegate/internal/stdlib"
)

// V2FrameMap maps v1 frame IDs (the stable identifier preserved across the
// v1→v2 migration) to the v2 bucket each frame belongs to. Built once at
// engine startup by walking the embedded v2 tree at internal/stdlib/v2/.
//
// The v1 frame ID stays canonical for audit-log continuity; the v2 bucket
// determines selection per the architecture spec §4 bucket-as-selection-unit
// composition semantics.
type V2FrameMap struct {
	IDToBucket map[string]buckets.Bucket
}

// BuildV2FrameMap walks the embedded v2 tree, parses each frame's frontmatter
// to extract the v1 ID (Category/Name), and produces the v1ID→bucket mapping.
// Returns an error if any v2 file fails to parse OR if the v2 path doesn't
// match the buckets.ParsePath rules (catches accidental violations of the
// 3-step-strict depth rule at startup).
func BuildV2FrameMap() (*V2FrameMap, error) {
	out := &V2FrameMap{IDToBucket: make(map[string]buckets.Bucket)}

	subFS, err := fs.Sub(stdlib.V2FS, "v2")
	if err != nil {
		return nil, fmt.Errorf("engine: open v2 embed subtree: %w", err)
	}

	err = fs.WalkDir(subFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		// Skip the classification README and any other underscore-prefixed
		// files (e.g. internal docs that aren't frame definitions).
		base := path[strings.LastIndex(path, "/")+1:]
		if strings.HasPrefix(base, "_") {
			return nil
		}

		// Parse the bucket path. The file's v2 location IS the bucket path
		// minus the .md suffix.
		bucketPath := strings.TrimSuffix(path, ".md")
		bucket, err := buckets.ParsePath(bucketPath)
		if err != nil {
			return fmt.Errorf("engine: invalid v2 bucket path %q: %w", bucketPath, err)
		}

		// Parse the frame's frontmatter to get the v1 ID (Category/Name).
		f, err := subFS.Open(path)
		if err != nil {
			return fmt.Errorf("engine: open v2 frame %s: %w", path, err)
		}
		frame, perr := frames.Parse(f, "v2:"+path)
		f.Close()
		if perr != nil {
			return fmt.Errorf("engine: parse v2 frame %s: %w", path, perr)
		}
		v1ID := frame.ID()
		if existing, dup := out.IDToBucket[v1ID]; dup {
			return fmt.Errorf("engine: v1 frame ID %q maps to two buckets: %s and %s", v1ID, existing.String(), bucket.String())
		}
		out.IDToBucket[v1ID] = bucket
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// EnabledFrameIDs resolves a v2.Config to the set of v1 frame IDs that
// should be active. Filters the supplied stdlib frame list by each frame's
// v2 bucket: a frame is enabled when buckets.Selection.IsFrameActive(bucket)
// returns true. Frames whose v1 ID isn't in the v2 map are skipped silently
// - those are v1-only frames that haven't been classified into v2 yet (the
// migration is in-progress).
func (m *V2FrameMap) EnabledFrameIDs(cfg *v2.Config, stdlibFrames []frames.Frame) []string {
	if cfg == nil || m == nil {
		return nil
	}
	sel := cfg.Selection()
	var enabled []string
	for _, f := range stdlibFrames {
		bucket, ok := m.IDToBucket[f.ID()]
		if !ok {
			// Frame not yet in v2 layout - skip. (Phase A3 classification
			// covered 44 frames; if more are added to v1 stdlib later, they
			// need v2 buckets too.)
			continue
		}
		if sel.IsFrameActive(bucket) {
			enabled = append(enabled, f.ID())
		}
	}
	return enabled
}
