// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package canonical

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoad_MalformedTOML must surface a parse error, not crash.
func TestLoad_MalformedTOML(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "bad.toml")
	if err := os.WriteFile(p, []byte("[unclosed section\nkey = \"value\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(p)
	if err == nil {
		t.Fatal("expected parse error on malformed TOML")
	}
}

// TestLoad_EmptyFile - zero-byte canonical table; should load without
// sections, not error.
func TestLoad_EmptyFile(t *testing.T) {
	tmp := t.TempDir()
	p := filepath.Join(tmp, "empty.toml")
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	tbl, err := Load(p)
	if err != nil {
		t.Fatalf("Load empty file: %v", err)
	}
	if _, ok := tbl.Section("anything"); ok {
		t.Error("empty file unexpectedly produced a section")
	}
}

// TestLoad_TopLevelScalarSkipped - keys at the top level (not inside a
// [section]) should be silently ignored (Table only exposes named sections).
func TestLoad_TopLevelScalarSkipped(t *testing.T) {
	tmp := t.TempDir()
	content := `
version = "1.0"
[folders]
"infra/" = "infra"
`
	p := filepath.Join(tmp, "mixed.toml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	tbl, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	folders, ok := tbl.Section("folders")
	if !ok {
		t.Fatal("folders section missing")
	}
	if folders["infra/"] != "infra" {
		t.Errorf("folders[infra/] = %q", folders["infra/"])
	}
}

// TestLoad_NonStringValuesStringified - integer/boolean values inside a
// section get stringified via fmt.Sprintf %v. Document the behaviour.
func TestLoad_NonStringValuesStringified(t *testing.T) {
	tmp := t.TempDir()
	content := `
[settings]
retries = 3
enabled = true
ratio = 0.75
`
	p := filepath.Join(tmp, "typed.toml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	tbl, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	settings, ok := tbl.Section("settings")
	if !ok {
		t.Fatal("settings section missing")
	}
	if settings["retries"] != "3" {
		t.Errorf("retries = %q, want %q", settings["retries"], "3")
	}
	if settings["enabled"] != "true" {
		t.Errorf("enabled = %q, want %q", settings["enabled"], "true")
	}
	if settings["ratio"] != "0.75" {
		t.Errorf("ratio = %q, want %q", settings["ratio"], "0.75")
	}
}

// TestLoad_NestedSubtableSkipped - current loader only surfaces top-level
// [sections]; subtables [section.sub] become a map[string]any and are
// silently skipped because they don't match the string-value coercion.
// Document this so users don't expect nested support.
func TestLoad_NestedSubtableBehavior(t *testing.T) {
	tmp := t.TempDir()
	content := `
[branches]
main = "production"

[branches.protected]
no-force = true
`
	p := filepath.Join(tmp, "nested.toml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	tbl, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	br, ok := tbl.Section("branches")
	if !ok {
		t.Fatal("branches section missing")
	}
	if br["main"] != "production" {
		t.Errorf("main = %q", br["main"])
	}
	// The `protected` key may appear stringified as a map (current behavior).
	// This test documents what comes out, not asserts a specific format.
	t.Logf("nested subtable serialization: protected = %q", br["protected"])
}

// TestLoad_UnicodeKeysAndValues
func TestLoad_UnicodeKeysAndValues(t *testing.T) {
	tmp := t.TempDir()
	content := `
[ids]
"日本.com" = "jp-001"
"münich.de" = "de-002"
`
	p := filepath.Join(tmp, "unicode.toml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	tbl, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	ids, ok := tbl.Section("ids")
	if !ok {
		t.Fatal("ids section missing")
	}
	if ids["日本.com"] != "jp-001" {
		t.Errorf("unicode key lost: %v", ids)
	}
}
