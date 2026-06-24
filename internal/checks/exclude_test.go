// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import "testing"

func TestIsExcluded_TopLevel(t *testing.T) {
	if !IsExcluded("/proj/node_modules/foo.js", "/proj", DefaultExcludes()) {
		t.Error("top-level node_modules file should be excluded")
	}
}

func TestIsExcluded_NestedSegment(t *testing.T) {
	if !IsExcluded("/proj/packages/sub/node_modules/dep.js", "/proj", DefaultExcludes()) {
		t.Error("nested node_modules path should be excluded")
	}
}

func TestIsExcluded_SimilarNameNotMatched(t *testing.T) {
	// `node_modules_old` and `dist-archive` must NOT be confused with the
	// real exclude segments. Whole-segment match only.
	cases := []string{
		"/proj/node_modules_old/foo.js",
		"/proj/my-dist-archive/x.js",
		"/proj/buildtime/lib.js",
	}
	for _, p := range cases {
		if IsExcluded(p, "/proj", DefaultExcludes()) {
			t.Errorf("similar-named segment incorrectly excluded: %s", p)
		}
	}
}

func TestIsExcluded_DotGitAndAppframes(t *testing.T) {
	cases := []string{
		"/proj/.git/HEAD",
		"/proj/.appframes/audit.log",
	}
	for _, p := range cases {
		if !IsExcluded(p, "/proj", DefaultExcludes()) {
			t.Errorf("expected exclusion for %s", p)
		}
	}
}

func TestIsExcluded_RelativePathInput(t *testing.T) {
	if !IsExcluded("./node_modules/foo.js", "", DefaultExcludes()) {
		t.Error("relative path with leading ./ should be excluded")
	}
	if !IsExcluded("node_modules/foo.js", "", DefaultExcludes()) {
		t.Error("bare relative path should be excluded")
	}
}

func TestIsExcluded_EmptyExcludesNoMatch(t *testing.T) {
	if IsExcluded("/proj/node_modules/foo.js", "/proj", nil) {
		t.Error("nil excludes should match nothing")
	}
	if IsExcluded("/proj/node_modules/foo.js", "/proj", []string{}) {
		t.Error("empty excludes should match nothing")
	}
}

func TestIsExcluded_CustomList(t *testing.T) {
	excludes := []string{"vendor", "target"}
	// "node_modules" is NOT in the custom list - must NOT match.
	if IsExcluded("/proj/node_modules/foo.js", "/proj", excludes) {
		t.Error("custom list should not match node_modules by default")
	}
	// "vendor" IS in the custom list.
	if !IsExcluded("/proj/vendor/dep.go", "/proj", excludes) {
		t.Error("custom list should match vendor")
	}
}

func TestIsExcluded_PathEqualsProjectRoot(t *testing.T) {
	if IsExcluded("/proj", "/proj", DefaultExcludes()) {
		t.Error("project root itself should never be 'inside an excluded dir'")
	}
}

func TestIsExcluded_AbsolutePathOutsideRoot(t *testing.T) {
	// If the path isn't under the project root at all, treat as-is.
	// Whether to exclude depends on segments anywhere in the path.
	if !IsExcluded("/somewhere/else/node_modules/foo.js", "/proj", DefaultExcludes()) {
		t.Error("path outside root with node_modules segment should still be excluded")
	}
}
