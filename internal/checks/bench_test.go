// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/engine"
)

// BenchmarkNoInnerHTML_1kFiles - full project scan with 1000 source files.
func BenchmarkNoInnerHTML_1kFiles(b *testing.B) {
	root := b.TempDir()
	for i := 0; i < 1000; i++ {
		content := "// safe " + strings.Repeat("x", 50) + "\nel.textContent = 'ok';\n"
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("f%04d.js", i)), []byte(content), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = NoInnerHTMLUserInput(engine.CheckContext{
			Trigger:     engine.TriggerCLI,
			ProjectRoot: root,
		})
	}
}

// BenchmarkCrossBranchID_500Files
func BenchmarkCrossBranchID_500Files(b *testing.B) {
	root := b.TempDir()
	dir := filepath.Join(root, ".appframes", "_canonical")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "website-ids.toml"),
		[]byte(`[ids]
"a.com" = "ok"
`), 0o644)
	for i := 0; i < 500; i++ {
		content := `<!doctype html><html><head>
<script data-website-id="ok"></script>
</head><body></body></html>`
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("p%03d.html", i)), []byte(content), 0o644); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = CrossBranchIDConsistency(engine.CheckContext{
			Trigger:     engine.TriggerCLI,
			ProjectRoot: root,
		})
	}
}

// BenchmarkFolderBranchLock_RepeatedLookup
func BenchmarkFolderBranchLock_RepeatedLookup(b *testing.B) {
	root := b.TempDir()
	dir := filepath.Join(root, ".appframes", "_canonical")
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, "folder-branch-map.toml"),
		[]byte(`[folders]
"./" = "main"
`), 0o644)
	ctx := engine.CheckContext{
		Trigger:       engine.TriggerGitWrap,
		ProjectRoot:   root,
		WorkingDir:    root,
		CurrentBranch: "main",
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = FolderBranchLock(ctx)
	}
}
