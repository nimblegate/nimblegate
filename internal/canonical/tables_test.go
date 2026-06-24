// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package canonical

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad_FolderBranchMap(t *testing.T) {
	tmp := t.TempDir()
	content := `
[folders]
"infra/" = "infra"
"landing/" = "landing"
"studio/" = "demo-studio"
`
	path := filepath.Join(tmp, "folder-branch-map.toml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	tbl, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}
	folders, ok := tbl.Section("folders")
	if !ok {
		t.Fatal("missing folders section")
	}
	if folders["infra/"] != "infra" {
		t.Errorf("folders[infra/] = %q", folders["infra/"])
	}
	if folders["studio/"] != "demo-studio" {
		t.Errorf("folders[studio/] = %q", folders["studio/"])
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "missing.toml"))
	if err == nil {
		t.Fatal("expected error for missing canonical table file")
	}
}
