// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
)

const noSmartQuotesDisableMarker = "appframes:disable encoding/no-smart-quotes-in-config"
const noSmartQuotesDisableLineMarker = "appframes:disable-next-line encoding/no-smart-quotes-in-config"
const noSmartQuotesMaxFileBytes = 1 << 20

var smartQuoteRunes = map[rune]string{
	0x2018: "U+2018 (left single curly quote)",
	0x2019: "U+2019 (right single curly quote)",
	0x201A: "U+201A (single low-9 quote)",
	0x201B: "U+201B (single high-reversed-9 quote)",
	0x201C: "U+201C (left double curly quote)",
	0x201D: "U+201D (right double curly quote)",
	0x201E: "U+201E (double low-9 quote)",
	0x201F: "U+201F (double high-reversed-9 quote)",
}

var configFileBasenames = map[string]bool{
	"compose.yaml":        true,
	"compose.yml":         true,
	"docker-compose.yaml": true,
	"docker-compose.yml":  true,
}

var configFileExtensions = map[string]bool{
	".toml": true,
	".yaml": true,
	".yml":  true,
	".json": true,
	".env":  true,
	".ini":  true,
}

func smartQuotesApplicableFile(path string) bool {
	if configFileBasenames[filepath.Base(path)] {
		return true
	}
	return configFileExtensions[strings.ToLower(filepath.Ext(path))]
}

// NoSmartQuotesInConfig flags typographic ("curly") quotes in config
// files. They look like ASCII quotes but parsers reject them.
func NoSmartQuotesInConfig(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "encoding/no-smart-quotes-in-config",
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
			if smartQuotesApplicableFile(path) {
				files = append(files, path)
			}
			return nil
		})
	}

	var hits []string
	var hitsStruct []engine.Hit
	const hitCap = 10

filesLoop:
	for _, file := range files {
		if ShouldSkipPath(ctx, file) {
			continue
		}
		if !smartQuotesApplicableFile(file) {
			continue
		}
		info, err := os.Stat(file)
		if err != nil || info.Size() > noSmartQuotesMaxFileBytes {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		content := string(data)
		if strings.Contains(content, noSmartQuotesDisableMarker) {
			continue
		}
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			if i > 0 && strings.Contains(lines[i-1], noSmartQuotesDisableLineMarker) {
				continue
			}
			for j := 0; j < len(line); {
				r, size := utf8.DecodeRuneInString(line[j:])
				if name, ok := smartQuoteRunes[r]; ok {
					hits = append(hits, fmt.Sprintf("%s:%d - %s", file, i+1, name))
					hitsStruct = append(hitsStruct, engine.Hit{File: file, Line: i + 1, Label: name})
					if len(hits) >= hitCap {
						break filesLoop
					}
					break
				}
				j += size
			}
		}
	}

	if len(hits) == 0 {
		res.Outcome = engine.OutcomePass
		return res
	}
	res.Outcome = engine.OutcomeBlock
	res.Reason = "smart / curly quotes in config (parsers reject these): " + strings.Join(hits, "; ")
	res.Fix = "replace U+2018-U+201F with ASCII `'` or `\"`. Common cause is paste from an LLM / word processor / doc page"
	res.Hits = hitsStruct
	return res
}
