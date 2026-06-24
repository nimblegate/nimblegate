// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"nimblegate/internal/config"
)

func writeAt(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadAny_v1WithFramesEnabled(t *testing.T) {
	// v1 schema - no [appframes.schema].version field.
	content := `
[frames]
enabled = ["core/no-force-push-main"]

[ui]
applied_kits = ["core", "web-app"]
`
	path := writeAt(t, "appframes.toml", content)
	result, err := config.ReadAny(path)
	if err != nil {
		t.Fatalf("ReadAny: %v", err)
	}
	if result.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1", result.SchemaVersion)
	}
	if result.V1 == nil {
		t.Fatal("V1 nil, expected non-nil ProjectConfig")
	}
	if result.V2 != nil {
		t.Error("V2 non-nil, expected nil for v1 source")
	}
}

func TestReadAny_v2(t *testing.T) {
	content := `
[appframes.schema]
version = 2

[framework]
selected = "html"

[platform]
selected = "cloudflare"

[domains]
selected = ["security"]
`
	path := writeAt(t, "appframes.toml", content)
	result, err := config.ReadAny(path)
	if err != nil {
		t.Fatalf("ReadAny: %v", err)
	}
	if result.SchemaVersion != 2 {
		t.Errorf("SchemaVersion = %d, want 2", result.SchemaVersion)
	}
	if result.V2 == nil {
		t.Fatal("V2 nil, expected non-nil v2.Config")
	}
	if result.V1 != nil {
		t.Error("V1 non-nil, expected nil for v2 source")
	}
}

func TestReadAny_explicitV1SchemaTagAlsoTreatedAsV1(t *testing.T) {
	// If someone sets [appframes.schema] version = 1 explicitly, treat as v1.
	content := `
[appframes.schema]
version = 1

[frames]
enabled = ["core/no-force-push-main"]
`
	path := writeAt(t, "appframes.toml", content)
	result, err := config.ReadAny(path)
	if err != nil {
		t.Fatalf("ReadAny: %v", err)
	}
	if result.SchemaVersion != 1 {
		t.Errorf("SchemaVersion = %d, want 1", result.SchemaVersion)
	}
}

func TestReadAny_rejectsFutureSchemaVersion(t *testing.T) {
	content := `
[appframes.schema]
version = 3

[frames]
enabled = []
`
	path := writeAt(t, "appframes.toml", content)
	_, err := config.ReadAny(path)
	if err == nil {
		t.Fatal("expected error for unknown future schema version")
	}
}

func TestReadAny_fileNotFound(t *testing.T) {
	_, err := config.ReadAny("/nonexistent/path/appframes.toml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
