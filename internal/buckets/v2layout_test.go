// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package buckets_test

import (
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/buckets"
)

// TestV2Layout_allFramesParseCleanly walks the v2 stdlib tree and confirms
// every frame file's path parses to a valid Bucket. Catches accidental
// directory typos or depth violations during the migration.
func TestV2Layout_allFramesParseCleanly(t *testing.T) {
	root := "../stdlib/v2"
	count := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		// Skip the classification README - it lives at v2/_classification.md, not a frame.
		base := filepath.Base(path)
		if strings.HasPrefix(base, "_") {
			return nil
		}
		// Strip the root prefix + .md suffix to get the bucket path
		rel := strings.TrimPrefix(path, root+"/")
		rel = strings.TrimSuffix(rel, ".md")
		b, err := buckets.ParsePath(rel)
		if err != nil {
			t.Errorf("ParsePath(%q): %v", rel, err)
			return nil
		}
		if b.FrameID == "" {
			t.Errorf("bucket %q has empty FrameID", rel)
		}
		count++
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir: %v", err)
	}
	if count != 44 {
		t.Errorf("walked %d frame files, want 44 (classification table)", count)
	}
}
