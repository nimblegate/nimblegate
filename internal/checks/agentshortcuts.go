// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"

	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
)

// The agent-shortcut frames catch ways a build is made to look green without
// being fixed: tests skipped, CI told to ignore failures, linters switched off
// wholesale. Gateway checks see only the pushed tree, so these flag what is
// present, not what a push added; intentional cases are exempted with a
// whitelist entry or an appframes:disable-next-line marker.

type lineRule struct {
	re    *regexp.Regexp
	label string
}

type lineScan struct {
	frameID  string
	rulesFor func(path string) []lineRule // nil or empty: file not scanned
	comment  func(trimmed string) bool    // lines to ignore entirely
	header   string
	fix      string
}

func (s lineScan) run(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{FrameID: s.frameID, Category: frames.CategoryAppCorrectness}
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
			if len(s.rulesFor(path)) > 0 {
				files = append(files, path)
			}
			return nil
		})
	}

	fileMarker := "appframes:disable " + s.frameID
	lineMarker := "appframes:disable-next-line " + s.frameID
	var hits []string
	var hitsStruct []engine.Hit
	const hitCap = 20
scan:
	for _, file := range files {
		rules := s.rulesFor(file)
		if len(rules) == 0 || ShouldSkipPath(ctx, file) {
			continue
		}
		data, ok := ReadFileBounded(file, DefaultMaxFileBytes)
		if !ok {
			continue
		}
		content := string(data)
		if fileDisabledByMarker(content, fileMarker) {
			continue
		}
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			if s.comment != nil && s.comment(strings.TrimSpace(line)) {
				continue
			}
			for _, r := range rules {
				if !r.re.MatchString(line) {
					continue
				}
				if i > 0 && lineCarriesMarker(lines[i-1], lineMarker) {
					break
				}
				hits = append(hits, fmt.Sprintf("%s:%d - %s", file, i+1, r.label))
				hitsStruct = append(hitsStruct, engine.Hit{File: file, Line: i + 1, Label: r.label})
				if len(hits) >= hitCap {
					break scan
				}
				break
			}
		}
	}

	if len(hits) == 0 {
		res.Outcome = engine.OutcomePass
		return res
	}
	res.Outcome = engine.OutcomeWarn
	res.Reason = s.header + ": " + strings.Join(hits, "; ")
	res.Fix = s.fix
	res.Hits = hitsStruct
	return res
}

// --- no-unmarked-test-skips ---

var jsTestExt = map[string]bool{".js": true, ".jsx": true, ".ts": true, ".tsx": true, ".mjs": true, ".cjs": true}

func inTestDir(path string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(filepath.Dir(path)), "/") {
		switch seg {
		case "test", "tests", "__tests__", "spec", "specs":
			return true
		}
	}
	return false
}

// testFileKind returns the language family of a test file, or "" if path is
// not a test file.
func testFileKind(path string) string {
	base := filepath.Base(path)
	ext := strings.ToLower(filepath.Ext(base))
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	switch {
	case jsTestExt[ext] && (strings.HasSuffix(stem, ".test") || strings.HasSuffix(stem, ".spec") || inTestDir(path)):
		return "js"
	case ext == ".py" && (strings.HasPrefix(base, "test_") || strings.HasSuffix(stem, "_test") || base == "conftest.py" || inTestDir(path)):
		return "py"
	case ext == ".go" && strings.HasSuffix(stem, "_test"):
		return "go"
	case ext == ".rs":
		return "rs"
	case (ext == ".java" || ext == ".kt") && (strings.HasSuffix(stem, "Test") || strings.HasSuffix(stem, "Tests") || strings.Contains(filepath.ToSlash(path), "/src/test/")):
		return "jvm"
	case ext == ".cs" && (strings.HasSuffix(stem, "Test") || strings.HasSuffix(stem, "Tests")):
		return "cs"
	case ext == ".rb" && (strings.HasSuffix(stem, "_spec") || strings.HasSuffix(stem, "_test")):
		return "rb"
	}
	return ""
}

var testSkipRules = map[string][]lineRule{
	"js": {
		{regexp.MustCompile(`\b(?:it|test|describe|context|suite)\.only\s*\(`), "focused test (.only) - the rest of the suite doesn't run"},
		{regexp.MustCompile(`\b(?:fit|fdescribe|ftest)\s*\(`), "focused test (fit/fdescribe) - the rest of the suite doesn't run"},
		{regexp.MustCompile(`\b(?:it|test|describe|context|suite)\.skip\s*\(`), "skipped test (.skip)"},
		{regexp.MustCompile(`\b(?:xit|xdescribe|xtest|xcontext)\s*\(`), "skipped test (xit/xdescribe)"},
	},
	"py": {
		{regexp.MustCompile(`@pytest\.mark\.skip\b(?:\(|\s|$)`), "skipped test (@pytest.mark.skip)"},
		{regexp.MustCompile(`@unittest\.skip\b(?:\(|\s|$)`), "skipped test (@unittest.skip)"},
		{regexp.MustCompile(`\bpytest\.skip\s*\(`), "skipped test (pytest.skip)"},
		{regexp.MustCompile(`\bself\.skipTest\s*\(`), "skipped test (skipTest)"},
	},
	"go": {
		{regexp.MustCompile(`\b[tb]\.Skip(?:f|Now)?\s*\(`), "skipped test (t.Skip)"},
	},
	"rs": {
		{regexp.MustCompile(`#\[ignore\b`), "ignored test (#[ignore])"},
	},
	"jvm": {
		{regexp.MustCompile(`@Disabled\b`), "disabled test (@Disabled)"},
		{regexp.MustCompile(`@Ignore\b`), "ignored test (@Ignore)"},
	},
	"cs": {
		{regexp.MustCompile(`\[(?:Fact|Theory)\s*\([^)]*\bSkip\s*=`), "skipped test (Skip =)"},
		{regexp.MustCompile(`\[Ignore\b`), "ignored test ([Ignore])"},
	},
	"rb": {
		{regexp.MustCompile(`^\s*(?:fit|fdescribe|fcontext)\b`), "focused spec (fit/fdescribe) - the rest of the suite doesn't run"},
		{regexp.MustCompile(`^\s*(?:xit|xdescribe|xcontext|xspecify)\b`), "skipped spec (xit/xdescribe)"},
		{regexp.MustCompile(`^\s*skip\b`), "skipped spec (skip)"},
	},
}

func isLineComment(trimmed string) bool {
	return strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "#[") ||
		strings.HasPrefix(trimmed, "*") || strings.HasPrefix(trimmed, "/*")
}

// NoUnmarkedTestSkips flags skipped and focused tests. A skipped test hides a
// failure; a focused one (.only, fit) silently stops the rest of the suite.
func NoUnmarkedTestSkips(ctx engine.CheckContext) engine.CheckResult {
	return lineScan{
		frameID:  "app-correctness/no-unmarked-test-skips",
		rulesFor: func(p string) []lineRule { return testSkipRules[testFileKind(p)] },
		comment:  isLineComment,
		header:   "skipped or focused tests",
		fix:      "fix the test and remove the skip, or record why it is skipped: a whitelist entry with a reason, or `// appframes:disable-next-line app-correctness/no-unmarked-test-skips` above the line",
	}.run(ctx)
}

// --- no-weakened-ci ---

func ciConfigFile(path string) bool {
	p := "/" + strings.TrimPrefix(filepath.ToSlash(path), "/")
	base := filepath.Base(p)
	ext := strings.ToLower(filepath.Ext(base))
	yaml := ext == ".yml" || ext == ".yaml"
	switch {
	case yaml && strings.Contains(p, "/.github/workflows/"),
		yaml && strings.Contains(p, "/.woodpecker/"),
		yaml && strings.HasSuffix(p, "/.circleci/config.yml"),
		yaml && strings.HasSuffix(p, "/.buildkite/pipeline.yml"):
		return true
	}
	switch base {
	case ".gitlab-ci.yml", "bitbucket-pipelines.yml", "azure-pipelines.yml", ".woodpecker.yml", ".drone.yml", "Jenkinsfile":
		return true
	}
	return false
}

var weakenedCIRules = []lineRule{
	{regexp.MustCompile(`\bcontinue-on-error:\s*true\b`), "continue-on-error: true - a failing step no longer fails the job"},
	{regexp.MustCompile(`\ballow_failure:\s*true\b`), "allow_failure: true - a failing job no longer fails the pipeline"},
	{regexp.MustCompile(`\|\|\s*true\b`), "|| true - the command's failure is ignored"},
	{regexp.MustCompile(`\|\|\s*exit\s+0\b`), "|| exit 0 - the command's failure is ignored"},
	{regexp.MustCompile(`\bset\s+\+e\b`), "set +e - later failing commands don't stop the step"},
}

// NoWeakenedCI flags CI settings that let failures pass.
func NoWeakenedCI(ctx engine.CheckContext) engine.CheckResult {
	return lineScan{
		frameID: "app-correctness/no-weakened-ci",
		rulesFor: func(p string) []lineRule {
			if ciConfigFile(p) {
				return weakenedCIRules
			}
			return nil
		},
		comment: func(t string) bool { return strings.HasPrefix(t, "#") || strings.HasPrefix(t, "//") },
		header:  "CI settings that let failures pass",
		fix:     "let the step fail and fix the cause, or record why it may fail: a whitelist entry with a reason, or `# appframes:disable-next-line app-correctness/no-weakened-ci` above the line",
	}.run(ctx)
}

// --- no-blanket-lint-disable ---

var lintSourceExt = map[string]bool{
	".js": true, ".jsx": true, ".ts": true, ".tsx": true, ".mjs": true, ".cjs": true, ".vue": true, ".svelte": true,
	".py": true, ".go": true, ".rs": true, ".java": true, ".kt": true,
}

var blanketLintRules = []lineRule{
	{regexp.MustCompile(`/\*\s*eslint-disable\s*\*/`), "eslint-disable for the whole file, no rule named"},
	{regexp.MustCompile(`//\s*@ts-nocheck\b`), "@ts-nocheck - type checking off for the whole file"},
	{regexp.MustCompile(`#\s*(?:ruff|flake8)\s*:\s*noqa\s*$`), "noqa for the whole file, no rule named"},
	{regexp.MustCompile(`#\s*pylint\s*:\s*(?:skip-file\b|disable\s*=\s*all\b)`), "pylint off for the whole file"},
	{regexp.MustCompile(`#\s*mypy\s*:\s*ignore-errors\b`), "mypy errors ignored for the whole file"},
	{regexp.MustCompile(`//\s*nolint\s*(?://.*)?$`), "//nolint without naming a linter"},
	{regexp.MustCompile(`#!\[allow\(\s*(?:warnings|clippy::all)\s*\)\]`), "all warnings allowed for the whole crate/module"},
	{regexp.MustCompile(`@SuppressWarnings\(\s*"all"\s*\)`), `@SuppressWarnings("all")`},
}

// NoBlanketLintDisable flags linters and type checkers switched off for a
// whole file (or crate), rather than for one named rule on one line.
func NoBlanketLintDisable(ctx engine.CheckContext) engine.CheckResult {
	return lineScan{
		frameID: "app-correctness/no-blanket-lint-disable",
		rulesFor: func(p string) []lineRule {
			if lintSourceExt[strings.ToLower(filepath.Ext(p))] {
				return blanketLintRules
			}
			return nil
		},
		header: "linters switched off wholesale",
		fix:    "fix the findings, or disable only the named rule on the line that needs it (`// eslint-disable-next-line no-console`, `# noqa: E501`, `//nolint:errcheck`); if the whole-file disable is intended, record why in a whitelist entry or with `appframes:disable-next-line app-correctness/no-blanket-lint-disable` above it",
	}.run(ctx)
}
