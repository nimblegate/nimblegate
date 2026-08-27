package gitwrap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// bufio.Scanner stops at a line over 64KB. Uninstall used to write back only
// the lines it had read, silently deleting the rest of the user's rc file.
// It must now refuse and leave the file untouched.
func TestUninstallRefusesRatherThanTruncate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	rc := filepath.Join(home, ".bashrc")
	original := "export EARLY=1\n" +
		BeginMarker + "\nwrapper line\n" + EndMarker + "\n" +
		strings.Repeat("x", 100*1024) + "\n" +
		"export MUST_SURVIVE=2\n"
	if err := os.WriteFile(rc, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Uninstall("bash"); err == nil {
		t.Error("Uninstall returned nil on an unreadable rc file; want an error")
	}

	after, err := os.ReadFile(rc)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != original {
		t.Errorf("rc file changed: %d bytes before, %d after", len(original), len(after))
	}
	if !strings.Contains(string(after), "MUST_SURVIVE") {
		t.Error("content after the over-long line was destroyed")
	}
}
