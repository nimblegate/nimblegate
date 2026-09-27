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
	rulesFor func(path string) []lineRule // nil or empty: no line rules for this file
	comment  func(trimmed string) bool    // lines to ignore entirely
	header   string
	fix      string
	// fileCheck, when set, inspects a whole file for patterns that span
	// lines (a Makefile target and its recipe). fileCheckFor selects files.
	fileCheck    func(lines []string) []engine.Hit
	fileCheckFor func(path string) bool
}

func (s lineScan) wants(path string) bool {
	return len(s.rulesFor(path)) > 0 || (s.fileCheckFor != nil && s.fileCheckFor(path))
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
			if s.wants(path) {
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
		if !s.wants(file) || ShouldSkipPath(ctx, file) {
			continue
		}
		rules := s.rulesFor(file)
		data, ok := ReadFileBounded(file, DefaultMaxFileBytes)
		if !ok {
			continue
		}
		content := string(data)
		if fileDisabledByMarker(content, fileMarker) {
			continue
		}
		lines := strings.Split(content, "\n")
		if s.fileCheck != nil && s.fileCheckFor(file) {
			for _, h := range s.fileCheck(lines) {
				if h.Line > 1 && lineCarriesMarker(lines[h.Line-2], lineMarker) {
					continue
				}
				hits = append(hits, fmt.Sprintf("%s:%d - %s", file, h.Line, h.Label))
				hitsStruct = append(hitsStruct, engine.Hit{File: file, Line: h.Line, Label: h.Label})
				if len(hits) >= hitCap {
					break scan
				}
			}
		}
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

// --- no-placeholder-tests ---

const jsTestName = `(?:'[^']*'|"[^"]*"|` + "`[^`]*`" + `)`

var placeholderTestRules = map[string][]lineRule{
	"js": {
		{regexp.MustCompile(`\b(?:it|test|specify)\s*\(\s*` + jsTestName + `\s*,\s*(?:async\s+)?(?:\(\s*\)\s*=>|function\s*\(\s*\))\s*\{\s*\}\s*\)`), "empty test body - it passes without testing anything"},
		{regexp.MustCompile(`expect\(\s*true\s*\)\s*\.\s*(?:toBe\(\s*true\s*\)|toEqual\(\s*true\s*\)|toBeTruthy\(\s*\))`), "expect(true) - an assertion that cannot fail"},
		{regexp.MustCompile(`expect\(\s*false\s*\)\s*\.\s*(?:toBe\(\s*false\s*\)|toBeFalsy\(\s*\))`), "expect(false) - an assertion that cannot fail"},
		{regexp.MustCompile(`expect\(\s*1\s*\)\s*\.\s*(?:toBe|toEqual)\(\s*1\s*\)`), "expect(1).toBe(1) - an assertion that cannot fail"},
		{regexp.MustCompile(`\bassert(?:\.ok|\.isTrue)?\(\s*true\s*\)`), "assert(true) - an assertion that cannot fail"},
	},
	"py": {
		{regexp.MustCompile(`^\s*def\s+test\w*\s*\([^)]*\)\s*(?:->\s*[\w\[\], .]+)?:\s*(?:pass|\.\.\.)\s*(?:#.*)?$`), "empty test body - it passes without testing anything"},
		{regexp.MustCompile(`^\s*assert\s+(?:True|1|1\s*==\s*1|not\s+False)\s*(?:#.*)?$`), "assert True - an assertion that cannot fail"},
		{regexp.MustCompile(`\bself\.assertTrue\(\s*True\s*\)`), "assertTrue(True) - an assertion that cannot fail"},
	},
	"go": {
		{regexp.MustCompile(`^\s*func\s+Test\w*\s*\(\s*\w+\s+\*testing\.T\s*\)\s*\{\s*\}`), "empty test function - it passes without testing anything"},
	},
	"rs": {
		{regexp.MustCompile(`\bassert!\(\s*true\s*\)`), "assert!(true) - an assertion that cannot fail"},
		{regexp.MustCompile(`\bassert_eq!\(\s*1\s*,\s*1\s*\)`), "assert_eq!(1, 1) - an assertion that cannot fail"},
	},
	"jvm": {
		{regexp.MustCompile(`\bassertTrue\(\s*true\s*\)`), "assertTrue(true) - an assertion that cannot fail"},
		{regexp.MustCompile(`\bassertEquals\(\s*1\s*,\s*1\s*\)`), "assertEquals(1, 1) - an assertion that cannot fail"},
	},
	"cs": {
		{regexp.MustCompile(`\bAssert\.(?:True|IsTrue)\(\s*true\s*\)`), "Assert.True(true) - an assertion that cannot fail"},
	},
	"rb": {
		{regexp.MustCompile(`^\s*it\s*\(?\s*(?:'[^']*'|"[^"]*")\s*\)?\s*(?:do\s*;?\s*end|\{\s*\})\s*$`), "empty spec - it passes without testing anything"},
		{regexp.MustCompile(`expect\(\s*true\s*\)\.to\s+(?:be\s+true|eq\(\s*true\s*\)|be_truthy)`), "expect(true) - an assertion that cannot fail"},
	},
}

// NoPlaceholderTests flags tests that cannot fail: empty bodies and
// assertions on constants. They make a suite look covered while testing
// nothing.
func NoPlaceholderTests(ctx engine.CheckContext) engine.CheckResult {
	return lineScan{
		frameID:  "app-correctness/no-placeholder-tests",
		rulesFor: func(p string) []lineRule { return placeholderTestRules[testFileKind(p)] },
		comment:  isLineComment,
		header:   "tests that cannot fail",
		fix:      "write the test's real assertion, or delete the placeholder; if it is intentional (a smoke test that only checks the file loads), record why: a whitelist entry with a reason, or `appframes:disable-next-line app-correctness/no-placeholder-tests` above it",
	}.run(ctx)
}

// --- no-test-special-casing ---

// jsQuote matches any JS string delimiter: ', " or a backtick.
const jsQuote = `['"` + "`" + `]`

var testCasingRules = map[string][]lineRule{
	"js": {
		{regexp.MustCompile(`process\.env\.NODE_ENV\s*[!=]==?\s*` + jsQuote + `test` + jsQuote), "branches on NODE_ENV === 'test'"},
		{regexp.MustCompile(jsQuote + `test` + jsQuote + `\s*[!=]==?\s*process\.env\.NODE_ENV`), "branches on NODE_ENV === 'test'"},
		{regexp.MustCompile(`process\.env\.(?:JEST_WORKER_ID|VITEST)\b`), "checks for the Jest/Vitest runner"},
		{regexp.MustCompile(`import\.meta\.env\.(?:VITEST\b|MODE\s*[!=]==?\s*` + jsQuote + `test` + jsQuote + `)`), "checks for the Vitest runner"},
		{regexp.MustCompile(`typeof\s+jest\s*[!=]==?\s*` + jsQuote + `undefined` + jsQuote), "checks for the Jest runner"},
	},
	"py": {
		{regexp.MustCompile(`['"]pytest['"]\s+(?:not\s+)?in\s+sys\.modules`), "checks whether pytest is loaded"},
		{regexp.MustCompile(`\bPYTEST_CURRENT_TEST\b`), "checks PYTEST_CURRENT_TEST"},
	},
	"go": {
		{regexp.MustCompile(`\btesting\.Testing\(\)`), "checks testing.Testing()"},
		{regexp.MustCompile(`flag\.Lookup\(\s*"test\.v"\s*\)`), "checks for the test.v flag"},
		{regexp.MustCompile(`strings\.HasSuffix\(\s*os\.Args\[0\]\s*,\s*"\.test"\s*\)`), "checks whether the binary is a test binary"},
	},
}

// productionSourceKind returns the language family of a non-test source
// file, or "" for test files, config files and other languages.
func productionSourceKind(path string) string {
	if testFileKind(path) != "" {
		return ""
	}
	base := filepath.Base(path)
	if strings.Contains(base, ".config.") || strings.HasPrefix(base, "setupTests") || base == "conftest.py" {
		return ""
	}
	switch strings.ToLower(filepath.Ext(base)) {
	case ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".vue", ".svelte":
		return "js"
	case ".py":
		return "py"
	case ".go":
		return "go"
	}
	return ""
}

// NoTestSpecialCasing flags production code that behaves differently when
// it detects a test run - the way to make a test pass without fixing the
// code under test.
func NoTestSpecialCasing(ctx engine.CheckContext) engine.CheckResult {
	return lineScan{
		frameID:  "app-correctness/no-test-special-casing",
		rulesFor: func(p string) []lineRule { return testCasingRules[productionSourceKind(p)] },
		comment:  isLineComment,
		header:   "production code that detects a test run",
		fix:      "make the code correct in both cases and inject the difference (a parameter, a fake, a config value) from the test instead; if the branch is intended (quieter logging under test), record why: a whitelist entry with a reason, or `appframes:disable-next-line app-correctness/no-test-special-casing` above it",
	}.run(ctx)
}

// --- no-noop-test-script ---

var noopNpmTest = regexp.MustCompile(`"test"\s*:\s*"\s*(?:exit\s+0|true|:|echo(?:[^"\\&;|]|\\.)*(?:&&\s*(?:exit\s+0|true))?)\s*"`)

func makefileName(path string) bool {
	switch filepath.Base(path) {
	case "Makefile", "makefile", "GNUmakefile":
		return true
	}
	return false
}

var makeTestTarget = regexp.MustCompile(`^test\s*:(?:[^=]|$)`)

// noopMakeTest finds a `test:` target with no prerequisites whose recipe
// only echoes or exits 0.
func noopMakeTest(lines []string) []engine.Hit {
	for i, line := range lines {
		if !makeTestTarget.MatchString(line) {
			continue
		}
		prereqs := strings.TrimSpace(strings.SplitN(strings.SplitN(line, ":", 2)[1], "#", 2)[0])
		if prereqs != "" {
			return nil
		}
		for _, r := range lines[i+1:] {
			if !strings.HasPrefix(r, "\t") {
				if strings.TrimSpace(r) == "" {
					continue
				}
				break
			}
			cmd := strings.TrimLeft(strings.TrimSpace(r), "@-+")
			cmd = strings.TrimSpace(cmd)
			if !(cmd == "" || cmd == "true" || cmd == ":" || cmd == "exit 0" || strings.HasPrefix(cmd, "echo ") || cmd == "echo" || strings.HasPrefix(cmd, "#")) {
				return nil
			}
		}
		return []engine.Hit{{Line: i + 1, Label: "make test runs nothing - its recipe only echoes or exits 0"}}
	}
	return nil
}

// NoNoopTestScript flags a test command that runs no tests: an npm `test`
// script of `exit 0` / `true` / only `echo`, or a Makefile `test:` target
// whose recipe does nothing. CI calling it then passes with nothing tested.
func NoNoopTestScript(ctx engine.CheckContext) engine.CheckResult {
	return lineScan{
		frameID: "app-correctness/no-noop-test-script",
		rulesFor: func(p string) []lineRule {
			if filepath.Base(p) == "package.json" {
				return []lineRule{{noopNpmTest, "npm test runs nothing - the script only echoes or exits 0"}}
			}
			return nil
		},
		fileCheck:    noopMakeTest,
		fileCheckFor: makefileName,
		header:       "test commands that run no tests",
		fix:          "point the test command at the real test runner; a project with no tests yet should let `test` fail (npm's default `exit 1`) rather than pass; if it is intended, record why in a whitelist entry",
	}.run(ctx)
}
