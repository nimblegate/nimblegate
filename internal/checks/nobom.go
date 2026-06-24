// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
)

const noBOMDisableMarker = "appframes:disable encoding/no-bom"
const noBOMMaxFileBytes = 1 << 20

var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// noBOMSkipExtensions are file extensions where a leading BOM is
// tolerated (Excel-emitted CSV/TSV is the only realistic case).
var noBOMSkipExtensions = map[string]bool{
	".csv": true,
	".tsv": true,
}

// NoBOM flags files that begin with EF BB BF (UTF-8 BOM). CSV/TSV are
// skipped because Excel writes a BOM by default and the files often
// round-trip through it.
func NoBOM(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "encoding/no-bom",
		Category: frames.CategoryEncoding,
	}
	files := ctx.ChangedFiles
	if len(files) == 0 && ctx.Trigger == engine.TriggerCLI {
		_ = filepath.WalkDir(ctx.ProjectRoot, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if ShouldSkipPath(ctx, path) {
					return filepath.SkipDir
				}
				return nil
			}
			files = append(files, path)
			return nil
		})
	}

	var hits []string
	var hitsStruct []engine.Hit
	const hitCap = 20

	for _, file := range files {
		if ShouldSkipPath(ctx, file) {
			continue
		}
		if noBOMSkipExtensions[strings.ToLower(filepath.Ext(file))] {
			continue
		}
		info, err := os.Stat(file)
		if err != nil || info.Size() < 3 || info.Size() > noBOMMaxFileBytes {
			continue
		}
		f, err := os.Open(file)
		if err != nil {
			continue
		}
		head := make([]byte, 3)
		_, err = f.Read(head)
		f.Close()
		if err != nil {
			continue
		}
		if !bytes.Equal(head, utf8BOM) {
			continue
		}
		// Look for file-level disable marker by reading more of the head.
		if data, err := os.ReadFile(file); err == nil {
			if strings.Contains(string(data), noBOMDisableMarker) {
				continue
			}
		}
		label := "UTF-8 BOM (EF BB BF) at file start"
		hits = append(hits, fmt.Sprintf("%s:1 - %s", file, label))
		hitsStruct = append(hitsStruct, engine.Hit{File: file, Line: 1, Label: label})
		if len(hits) >= hitCap {
			break
		}
	}

	if len(hits) == 0 {
		res.Outcome = engine.OutcomePass
		return res
	}
	res.Outcome = engine.OutcomeBlock
	res.Reason = "UTF-8 BOM at file start (breaks shebangs / JSON parsers / migrations): " + strings.Join(hits, "; ")
	res.Fix = "strip the BOM: `sed -i '1s/^\\xEF\\xBB\\xBF//' <file>` or save the file as UTF-8 without BOM. CSV/TSV are already exempt; for other files that legitimately need a BOM, add `# appframes:disable encoding/no-bom`"
	res.Hits = hitsStruct
	return res
}
