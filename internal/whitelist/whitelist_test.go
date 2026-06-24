// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package whitelist

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeWhitelist(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "whitelist.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func mustParseDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func knownFrames() map[string]bool {
	return map[string]bool{
		"security/no-hardcoded-credentials": true,
		"security/no-private-keys-in-repo":  true,
		"command-safety/curl-pipe-shell":    true,
	}
}

// ----- Load / fail-closed cases ------------------------------------------------

func TestLoad_MissingFileReturnsNilNoError(t *testing.T) {
	// Spec: missing whitelist.toml is silent - "no exemptions" is fine.
	w, err := Load(filepath.Join(t.TempDir(), "nope.toml"), knownFrames(), time.Now())
	if err != nil {
		t.Fatalf("missing file should not error; got %v", err)
	}
	if w != nil {
		t.Fatalf("missing file should return nil Whitelist; got %#v", w)
	}
}

func TestLoad_MalformedTOMLHardError(t *testing.T) {
	path := writeWhitelist(t, "this is not toml [[[")
	_, err := Load(path, knownFrames(), time.Now())
	if err == nil {
		t.Fatal("malformed TOML should fail-closed; got nil error")
	}
}

func TestLoad_MissingReasonHardError(t *testing.T) {
	// Spec §6: `reason:` is required (audit-grade).
	path := writeWhitelist(t, `
[[entry]]
frame = "security/no-hardcoded-credentials"
path  = "vendor/**"
`)
	_, err := Load(path, knownFrames(), time.Now())
	if err == nil {
		t.Fatal("missing reason should fail-closed; got nil error")
	}
	if !strings.Contains(err.Error(), "reason") {
		t.Errorf("error should mention 'reason'; got %v", err)
	}
}

func TestLoad_BadExpiresFormatHardError(t *testing.T) {
	path := writeWhitelist(t, `
[[entry]]
frame   = "security/no-hardcoded-credentials"
path    = "vendor/**"
reason  = "vendored"
expires = "next tuesday"
`)
	_, err := Load(path, knownFrames(), time.Now())
	if err == nil {
		t.Fatal("bad expires format should fail-closed; got nil error")
	}
}

func TestLoad_UnknownFrameIDHardError(t *testing.T) {
	path := writeWhitelist(t, `
[[entry]]
frame   = "security/no-such-frame"
path    = "vendor/**"
reason  = "typo demo"
`)
	_, err := Load(path, knownFrames(), time.Now())
	if err == nil {
		t.Fatal("unknown frame ID should fail-closed; got nil error")
	}
	if !strings.Contains(err.Error(), "unknown frame") {
		t.Errorf("error should say 'unknown frame'; got %v", err)
	}
}

func TestLoad_UnknownCategoryWildcardHardError(t *testing.T) {
	path := writeWhitelist(t, `
[[entry]]
frame   = "made-up-category/*"
path    = "**"
reason  = "covers nothing"
`)
	_, err := Load(path, knownFrames(), time.Now())
	if err == nil {
		t.Fatal("unknown category wildcard should fail-closed; got nil error")
	}
}

func TestLoad_StarFrameAccepted(t *testing.T) {
	path := writeWhitelist(t, `
[[entry]]
frame  = "*"
path   = "vendor/**"
reason = "all frames silenced under vendor/"
`)
	w, err := Load(path, knownFrames(), time.Now())
	if err != nil {
		t.Fatalf("\"*\" frame should load; got %v", err)
	}
	if w.Count() != 1 {
		t.Errorf("Count = %d, want 1", w.Count())
	}
}

func TestLoad_EmptyPathDefaultsToDoubleStar(t *testing.T) {
	path := writeWhitelist(t, `
[[entry]]
frame  = "security/no-hardcoded-credentials"
reason = "no path means anywhere"
`)
	w, err := Load(path, knownFrames(), time.Now())
	if err != nil {
		t.Fatalf("missing path should default; got %v", err)
	}
	if !w.Match("security/no-hardcoded-credentials", "anywhere/at/all.txt", "label") {
		t.Error("missing path should match any file (default \"**\")")
	}
}

// ----- Match behavior ---------------------------------------------------------

func TestMatch_ExactFrameAndPath(t *testing.T) {
	path := writeWhitelist(t, `
[[entry]]
frame  = "security/no-hardcoded-credentials"
path   = "test/fixtures/**"
reason = "intentional fake credentials in fixtures"
`)
	w, err := Load(path, knownFrames(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !w.Match("security/no-hardcoded-credentials", "test/fixtures/secrets.env", "AWS access key") {
		t.Error("expected fixture path to match")
	}
	if w.Match("security/no-hardcoded-credentials", "src/main.go", "AWS access key") {
		t.Error("non-fixture path should not match")
	}
	if w.Match("security/no-private-keys-in-repo", "test/fixtures/secrets.env", "PEM RSA") {
		t.Error("different frame should not match")
	}
}

func TestMatch_CategoryWildcard(t *testing.T) {
	path := writeWhitelist(t, `
[[entry]]
frame  = "security/*"
path   = "vendor/**"
reason = "vendored code, all security frames silenced"
`)
	w, err := Load(path, knownFrames(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !w.Match("security/no-hardcoded-credentials", "vendor/lib/x.go", "any") {
		t.Error("category wildcard should match same-category frame")
	}
	if !w.Match("security/no-private-keys-in-repo", "vendor/lib/x.pem", "any") {
		t.Error("category wildcard should match other same-category frame")
	}
	if w.Match("command-safety/curl-pipe-shell", "vendor/lib/install.sh", "any") {
		t.Error("category wildcard should NOT match different category")
	}
}

func TestMatch_PatternSubstring(t *testing.T) {
	path := writeWhitelist(t, `
[[entry]]
frame   = "command-safety/curl-pipe-shell"
path    = "scripts/**"
pattern = "myapp.example.com"
reason  = "vetted bootstrap from our own host"
`)
	w, err := Load(path, knownFrames(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !w.Match("command-safety/curl-pipe-shell", "scripts/install.sh",
		"vetted bootstrap | curl https://myapp.example.com/install.sh | bash") {
		t.Error("label containing pattern substring should match")
	}
	if w.Match("command-safety/curl-pipe-shell", "scripts/install.sh",
		"some other curl | bash from sketchy.evil.com") {
		t.Error("label NOT containing pattern should not match")
	}
}

// ----- Expiry ------------------------------------------------------------------

func TestExpiry_PastDateMakesEntryInactive(t *testing.T) {
	path := writeWhitelist(t, `
[[entry]]
frame   = "security/no-hardcoded-credentials"
path    = "**"
reason  = "expired exemption"
expires = "2020-01-01"
`)
	today := mustParseDate(t, "2026-05-18")
	w, err := Load(path, knownFrames(), today)
	if err != nil {
		t.Fatal(err)
	}
	if w.Match("security/no-hardcoded-credentials", "any.go", "any") {
		t.Error("expired entry should NOT suppress hits (it's inactive)")
	}
	rep := w.Hygiene()
	if rep.Expired != 1 {
		t.Errorf("Hygiene.Expired = %d, want 1", rep.Expired)
	}
}

func TestExpiry_FutureDateActive(t *testing.T) {
	path := writeWhitelist(t, `
[[entry]]
frame   = "security/no-hardcoded-credentials"
path    = "**"
reason  = "active exemption"
expires = "2099-12-31"
`)
	today := mustParseDate(t, "2026-05-18")
	w, err := Load(path, knownFrames(), today)
	if err != nil {
		t.Fatal(err)
	}
	if !w.Match("security/no-hardcoded-credentials", "any.go", "any") {
		t.Error("active entry should suppress")
	}
}

// ----- Hygiene ----------------------------------------------------------------

func TestHygiene_UnusedEntry(t *testing.T) {
	path := writeWhitelist(t, `
[[entry]]
frame  = "security/no-hardcoded-credentials"
path   = "never/matches/**"
reason = "this entry never fires"
`)
	w, err := Load(path, knownFrames(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	// No Match calls → entry should be reported unused.
	rep := w.Hygiene()
	if rep.Active != 1 {
		t.Errorf("Active = %d, want 1", rep.Active)
	}
	if len(rep.Unused) != 1 {
		t.Errorf("Unused entries = %d, want 1", len(rep.Unused))
	}
}

func TestHygiene_MatchedEntryNotUnused(t *testing.T) {
	path := writeWhitelist(t, `
[[entry]]
frame  = "security/no-hardcoded-credentials"
path   = "vendor/**"
reason = "matched at least once"
`)
	w, err := Load(path, knownFrames(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !w.Match("security/no-hardcoded-credentials", "vendor/lib/x.go", "label") {
		t.Fatal("expected match")
	}
	rep := w.Hygiene()
	if len(rep.Unused) != 0 {
		t.Errorf("matched entry should not appear in Unused; got %v", rep.Unused)
	}
}

// ----- Specificity / attribution ---------------------------------------------

func TestSpecificity_ExactFrameWinsOverCategoryWildcard(t *testing.T) {
	// Both entries cover the same hit. Spec says most-specific entry
	// gets the match credit so hygiene reports the right "unused".
	path := writeWhitelist(t, `
[[entry]]
frame  = "security/*"
path   = "**"
reason = "broad category exemption"

[[entry]]
frame  = "security/no-hardcoded-credentials"
path   = "**"
reason = "narrow exemption - should win attribution"
`)
	w, err := Load(path, knownFrames(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if !w.Match("security/no-hardcoded-credentials", "x.go", "label") {
		t.Fatal("expected match")
	}
	rep := w.Hygiene()
	// The broad entry should appear in Unused (it never got attribution).
	if len(rep.Unused) != 1 {
		t.Fatalf("Unused = %d, want 1 (broad entry should be unused)", len(rep.Unused))
	}
	if rep.Unused[0].Frame != "security/*" {
		t.Errorf("Unused frame = %q, want \"security/*\" (broad lost attribution)", rep.Unused[0].Frame)
	}
}

func TestRemoveEntry_removesMatching(t *testing.T) {
	path := writeWhitelist(t, `
[[entry]]
frame  = "security/no-hardcoded-credentials"
path   = "internal/x_test.go"
reason = "fixture keys"

[[entry]]
frame  = "convention/todo"
path   = "docs/notes.md"
pattern = "TODO\\("
reason = "tracked separately"
expires = "2026-12-01"

[[entry]]
frame  = "security/no-private-keys-in-repo"
path   = "internal/y_test.go"
reason = "fixture keys"
`)
	removed, err := RemoveEntry(path, "convention/todo", "docs/notes.md")
	if err != nil || !removed {
		t.Fatalf("RemoveEntry: removed=%v err=%v", removed, err)
	}
	data, _ := os.ReadFile(path)
	body := string(data)
	if strings.Contains(body, "convention/todo") {
		t.Fatalf("removed entry still present:\n%s", body)
	}
	// Surviving entries keep their full data, including pattern/expires-free
	// neighbors and the no-private-keys entry below it.
	for _, want := range []string{
		`security/no-hardcoded-credentials`,
		`internal/x_test.go`,
		`security/no-private-keys-in-repo`,
		`internal/y_test.go`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("post-remove body missing %q:\n%s", want, body)
		}
	}
}

func TestRemoveEntry_noMatchReturnsFalse(t *testing.T) {
	path := writeWhitelist(t, `
[[entry]]
frame  = "security/no-hardcoded-credentials"
path   = "x.go"
reason = "r"
`)
	removed, err := RemoveEntry(path, "no-such/frame", "x.go")
	if err != nil || removed {
		t.Fatalf("RemoveEntry: removed=%v err=%v", removed, err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), `security/no-hardcoded-credentials`) {
		t.Fatal("file should be untouched on no-match")
	}
}

func TestRemoveEntry_missingFileReturnsFalse(t *testing.T) {
	removed, err := RemoveEntry("/nonexistent/whitelist.toml", "x/y", "p")
	if err != nil || removed {
		t.Fatalf("RemoveEntry on missing file: removed=%v err=%v", removed, err)
	}
}

func TestRemoveEntry_preservesPatternAndExpires(t *testing.T) {
	path := writeWhitelist(t, `
[[entry]]
frame  = "doomed/one"
path   = "drop.go"
reason = "go away"

[[entry]]
frame  = "convention/todo"
path   = "keep.md"
pattern = "TODO\\("
reason = "still tracked"
expires = "2026-12-01"
`)
	if _, err := RemoveEntry(path, "doomed/one", "drop.go"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	body := string(data)
	for _, want := range []string{`pattern = "TODO\\("`, `expires = "2026-12-01"`} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q in:\n%s", want, body)
		}
	}
}
