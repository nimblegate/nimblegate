// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"strings"
	"testing"

	"nimblegate/internal/engine"
)

func TestAgentShortcuts_ProjectWalkFindsEach(t *testing.T) {
	root := t.TempDir()
	writeWeb(t, root, "web/src/cart.test.ts", "it.only('total', () => {});\n")
	writeWeb(t, root, ".github/workflows/ci.yml", "jobs:\n  t:\n    steps:\n      - run: make test || true\n")
	writeWeb(t, root, "api/handler.py", "# pylint: disable=all\n")
	writeWeb(t, root, "node_modules/lib/x.test.js", "it.skip('vendored', () => {});\n")

	for name, tc := range map[string]struct {
		check func(engine.CheckContext) engine.CheckResult
		file  string
	}{
		"test-skips":   {NoUnmarkedTestSkips, "cart.test.ts"},
		"weakened-ci":  {NoWeakenedCI, "ci.yml"},
		"blanket-lint": {NoBlanketLintDisable, "handler.py"},
	} {
		got := tc.check(cliCtx(root))
		if got.Outcome != engine.OutcomeWarn || !strings.Contains(got.Reason, tc.file) {
			t.Errorf("%s: outcome %s, reason %q; want WARN naming %s", name, got.Outcome, got.Reason, tc.file)
		}
		if strings.Contains(got.Reason, "node_modules") {
			t.Errorf("%s: scanned an excluded directory: %s", name, got.Reason)
		}
	}
}

func TestAgentShortcuts_FileLevelOptOut(t *testing.T) {
	root := t.TempDir()
	writeWeb(t, root, "gen/client.ts", "// appframes:disable app-correctness/no-blanket-lint-disable - generated\n// @ts-nocheck\n")
	if got := NoBlanketLintDisable(cliCtx(root)); got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s; want PASS with a file-level disable\nreason: %s", got.Outcome, got.Reason)
	}
}

func TestTestFileKind(t *testing.T) {
	for path, want := range map[string]string{
		"src/cart.test.ts":            "js",
		"src/__tests__/cart.js":       "js",
		"src/cart.ts":                 "",
		"tests/test_cart.py":          "py",
		"app/cart.py":                 "",
		"store/save_test.go":          "go",
		"store/save.go":               "",
		"src/lib.rs":                  "rs",
		"src/test/java/CartTest.java": "jvm",
		"Cart.Tests/CartTests.cs":     "cs",
		"spec/cart_spec.rb":           "rb",
	} {
		if got := testFileKind(path); got != want {
			t.Errorf("testFileKind(%q) = %q; want %q", path, got, want)
		}
	}
}

func TestCIConfigFile(t *testing.T) {
	for path, want := range map[string]bool{
		"/r/.github/workflows/ci.yml":  true,
		"/r/.github/workflows/ci.yaml": true,
		"/r/.gitlab-ci.yml":            true,
		"/r/.circleci/config.yml":      true,
		"/r/Jenkinsfile":               true,
		"/r/.github/dependabot.yml":    false,
		"/r/deploy/docker-compose.yml": false,
		"/r/scripts/ci.sh":             false,
		".github/workflows/ci.yml":     true,
		".circleci/config.yml":         true,
	} {
		if got := ciConfigFile(path); got != want {
			t.Errorf("ciConfigFile(%q) = %v; want %v", path, got, want)
		}
	}
}
