// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package frames

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestSchemaFile_IsValidJSON confirms the published frame frontmatter JSON
// Schema parses as valid JSON. Tighter validation (verifying it's a proper
// JSON Schema doc) would require a JSON Schema validator dependency; this
// keeps the test simple while still catching syntax breaks in commits.
func TestSchemaFile_IsValidJSON(t *testing.T) {
	wd, _ := os.Getwd()
	// Walk up to repo root.
	repoRoot := filepath.Clean(filepath.Join(wd, "..", ".."))
	schemaPath := filepath.Join(repoRoot, "docs", "schemas", "frame-frontmatter.schema.json")
	data, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	// Sanity check key shape.
	if doc["$schema"] == nil {
		t.Error("schema missing $schema")
	}
	if doc["title"] != "nimblegate frame frontmatter" {
		t.Errorf("title = %q", doc["title"])
	}
	props, ok := doc["properties"].(map[string]any)
	if !ok {
		t.Fatal("missing properties block")
	}
	for _, required := range []string{"name", "category", "severity", "triggers"} {
		if _, ok := props[required].(map[string]any); !ok {
			t.Errorf("schema missing property definition for %q", required)
		}
	}
}
