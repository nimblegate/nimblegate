// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"nimblegate/internal/engine"
)

// Keys here are assembled at run time from a seeded generator, so this file
// holds no literal that looks like a credential.
func fakeToken(prefix string, n int, seed int64) string {
	const alnum = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
	r := rand.New(rand.NewSource(seed))
	b := make([]byte, n)
	for i := range b {
		b[i] = alnum[r.Intn(len(alnum))]
	}
	return prefix + string(b)
}

func stripeLive(seed int64) string { return fakeToken("sk_"+"live_", 28, seed) }
func awsAccess(seed int64) string {
	const upper = "ABCDEFGHJKLMNPQRSTUVWXYZ234567"
	r := rand.New(rand.NewSource(seed))
	b := make([]byte, 16)
	for i := range b {
		b[i] = upper[r.Intn(len(upper))]
	}
	return "AK" + "IA" + string(b)
}

// filler is minified-looking JavaScript without anything a secret frame
// reports: identifiers, punctuation and short strings.
func filler(n int, seed int64) string {
	r := rand.New(rand.NewSource(seed))
	parts := []string{"function(e,t){return e+t}", "var a=b||{};", "c.push(d);", `x="ok";`, "if(n>3){m()}", "o.p=q.r;"}
	var sb strings.Builder
	for sb.Len() < n {
		sb.WriteString(parts[r.Intn(len(parts))])
	}
	return sb.String()[:n]
}

func writeBundle(t *testing.T, root, rel, body string) string {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func winCtx(root string) engine.CheckContext {
	return engine.CheckContext{Trigger: engine.TriggerCLI, ProjectRoot: root, ExcludedDirs: DefaultExcludes()}
}

var columnRe = regexp.MustCompile(`:(\d+) - ([^;]+?) \(column (\d+)\)`)

// TestScanTextWindows_EveryLineSeenWithItsNumber: across window flushes,
// overlap carries and a line longer than a window, every line reaches the
// caller with the right number, and the pieces of the long line rebuild it.
func TestScanTextWindows_EveryLineSeenWithItsNumber(t *testing.T) {
	var lines []string
	for i := 0; i < 30000; i++ {
		lines = append(lines, fmt.Sprintf("line %d %s", i+1, filler(40, int64(i))))
	}
	long := filler(3*textWindowBytes+12345, 99)
	lines = append(lines[:20000], append([]string{long}, lines[20000:]...)...)
	content := strings.Join(lines, "\n")

	got := map[int]string{}
	var longPieces []textWindow
	if err := scanTextWindows(strings.NewReader(content), func(w textWindow) bool {
		if w.Cut {
			longPieces = append(longPieces, w)
			return true
		}
		for i, l := range w.Lines {
			n := w.FirstLine + i
			if prev, ok := got[n]; ok && prev != l {
				t.Fatalf("line %d seen twice with different text", n)
			}
			got[n] = l
		}
		return true
	}); err != nil {
		t.Fatal(err)
	}
	for i, l := range lines {
		if i == 20000 {
			continue
		}
		if got[i+1] != l {
			t.Fatalf("line %d: got %.30q want %.30q", i+1, got[i+1], l)
		}
	}
	if len(longPieces) < 4 {
		t.Fatalf("long line came in %d pieces, want at least 4", len(longPieces))
	}
	var rebuilt strings.Builder
	for _, w := range longPieces {
		if w.FirstLine != 20001 {
			t.Fatalf("long-line piece numbered %d, want 20001", w.FirstLine)
		}
		if w.Prev != lines[19999] {
			t.Fatalf("long-line piece prev = %.30q, want the line above", w.Prev)
		}
		skip := rebuilt.Len() - (w.FirstCol - 1)
		if skip < 0 || skip > len(w.Lines[0]) {
			t.Fatalf("piece at column %d leaves a gap (rebuilt %d bytes)", w.FirstCol, rebuilt.Len())
		}
		rebuilt.WriteString(w.Lines[0][skip:])
	}
	if rebuilt.String() != long {
		t.Fatalf("pieces rebuild %d bytes, want %d", rebuilt.Len(), len(long))
	}
}

// TestCredentials_KeyAtEndOfLargeMinifiedBundle is the case a 1 MiB cap let
// through: one 1.5 MB line with a live key at its end.
func TestCredentials_KeyAtEndOfLargeMinifiedBundle(t *testing.T) {
	root := t.TempDir()
	body := filler(1500000, 1) + `;const s="` + stripeLive(1) + `";`
	writeBundle(t, root, "web/assets/index-a1b2c3.js", body)
	got := NoHardcodedCredentials(winCtx(root))
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s, want BLOCK: %s", got.Outcome, got.Reason)
	}
	m := columnRe.FindStringSubmatch(got.Reason)
	if m == nil || m[1] != "1" || m[2] != "Stripe secret key (live)" {
		t.Fatalf("want line 1 with a column, got %s", got.Reason)
	}
	if want := fmt.Sprint(len(filler(1500000, 1)) + len(`;const s="`) + 1); m[3] != want {
		t.Fatalf("column = %s, want %s", m[3], want)
	}
	if strings.Contains(got.Reason, stripeLive(1)) {
		t.Fatalf("REDACTION FAILURE: %s", got.Reason)
	}
}

// TestCredentials_EveryKeyOnOneMinifiedLine: rotating the first key must not
// leave a second one unreported.
func TestCredentials_EveryKeyOnOneMinifiedLine(t *testing.T) {
	for _, size := range []int{5000, 1500000} {
		root := t.TempDir()
		body := filler(size, 2) + `a="` + stripeLive(2) + `";` + filler(3000, 3) + `b="` + awsAccess(3) + `";` + filler(3000, 4) + `c="` + stripeLive(4) + `";`
		writeBundle(t, root, "public/app.js", body)
		got := NoHardcodedCredentials(winCtx(root))
		ms := columnRe.FindAllStringSubmatch(got.Reason, -1)
		if got.Outcome != engine.OutcomeBlock || len(ms) != 3 {
			t.Fatalf("size %d: want 3 findings with columns, got %s: %s", size, got.Outcome, got.Reason)
		}
		if len(got.Hits) != 3 {
			t.Fatalf("size %d: %d hits, want 3", size, len(got.Hits))
		}
	}
}

// TestCredentials_KeyAcrossAWindowCut: a key that straddles the cut between
// two pieces of a long line is found, once.
func TestCredentials_KeyAcrossAWindowCut(t *testing.T) {
	for _, off := range []int{-20, -5, 0, 3, textOverlapBytes - 10} {
		root := t.TempDir()
		start := textWindowBytes + off
		body := filler(start, 5) + stripeLive(5) + `";` + filler(400000, 6)
		writeBundle(t, root, "web/main.js", body)
		got := NoHardcodedCredentials(winCtx(root))
		ms := columnRe.FindAllStringSubmatch(got.Reason, -1)
		if len(ms) != 1 || ms[0][3] != fmt.Sprint(start+1) {
			t.Fatalf("offset %d: want one finding at column %d, got %s", off, start+1, got.Reason)
		}
	}
}

// TestCredentials_LargeMultiLineFile: line numbers stay right across window
// flushes, including a key inside the carried overlap.
func TestCredentials_LargeMultiLineFile(t *testing.T) {
	root := t.TempDir()
	var lines []string
	for i := 0; i < 40000; i++ {
		lines = append(lines, filler(60, int64(i)))
	}
	// Around the first flush: lines of 61 bytes, so it falls near line 17190.
	want := map[int]bool{17180: true, 17200: true, 39000: true}
	for n := range want {
		lines[n-1] = `k = "` + stripeLive(int64(n)) + `"`
	}
	writeBundle(t, root, "data/export.txt", strings.Join(lines, "\n"))
	got := NoHardcodedCredentials(winCtx(root))
	if len(got.Hits) != len(want) {
		t.Fatalf("%d hits, want %d: %s", len(got.Hits), len(want), got.Reason)
	}
	for _, h := range got.Hits {
		if !want[h.Line] || strings.Contains(h.Label, "column") {
			t.Fatalf("unexpected hit %+v", h)
		}
	}
}

// TestCredentials_LargeFileMarkers: the disable-next-line marker above a long
// line still suppresses it, and a file-level marker anywhere in a large file
// switches the frame off for that file.
func TestCredentials_LargeFileMarkers(t *testing.T) {
	root := t.TempDir()
	marker := "// appframes:" + "disable-next-line security/no-hardcoded-credentials"
	writeBundle(t, root, "a.js", marker+"\n"+filler(1200000, 7)+stripeLive(7)+"\n")
	got := NoHardcodedCredentials(winCtx(root))
	if got.Outcome != engine.OutcomePass {
		t.Fatalf("next-line marker: outcome %s: %s", got.Outcome, got.Reason)
	}

	root = t.TempDir()
	fileMarker := "// appframes:" + "disable security/no-hardcoded-credentials"
	writeBundle(t, root, "b.js", filler(1200000, 8)+stripeLive(8)+"\n"+fileMarker+"\n")
	got = NoHardcodedCredentials(winCtx(root))
	if got.Outcome != engine.OutcomePass {
		t.Fatalf("file marker: outcome %s: %s", got.Outcome, got.Reason)
	}
}

// TestCredentials_LargeBinarySkipped: big binary files are not text; a NUL
// byte or a binary extension skips them, as a small binary's matches are
// still reported (unchanged behaviour).
func TestCredentials_LargeBinarySkipped(t *testing.T) {
	root := t.TempDir()
	writeBundle(t, root, "img.png", filler(1200000, 9)+stripeLive(9))
	writeBundle(t, root, "blob.dat", filler(600000, 10)+"\x00"+filler(600000, 11)+stripeLive(10))
	got := NoHardcodedCredentials(winCtx(root))
	if got.Outcome != engine.OutcomePass {
		t.Fatalf("outcome %s: %s", got.Outcome, got.Reason)
	}
}

// TestCredentials_TooLargeWarns: a text file past the cap is named, not
// passed over in silence.
func TestCredentials_TooLargeWarns(t *testing.T) {
	root := t.TempDir()
	p := writeBundle(t, root, "dump.sql", "x\n")
	if err := os.Truncate(p, secretScanMaxBytes+1); err != nil {
		t.Fatal(err)
	}
	got := NoHardcodedCredentials(winCtx(root))
	if got.Outcome != engine.OutcomeWarn || !strings.Contains(got.Reason, "dump.sql:0 - "+tooLargeLabel) {
		t.Fatalf("outcome %s: %s", got.Outcome, got.Reason)
	}

	writeBundle(t, root, "src/k.go", `k := "`+stripeLive(12)+`"`+"\n")
	got = NoHardcodedCredentials(winCtx(root))
	if got.Outcome != engine.OutcomeBlock || !strings.Contains(got.Reason, tooLargeLabel) {
		t.Fatalf("with a key too: outcome %s: %s", got.Outcome, got.Reason)
	}
}

// TestCredentials_TwoKeysOnShortLine: an ordinary line with two keys reports
// both, without a column.
func TestCredentials_TwoKeysOnShortLine(t *testing.T) {
	got := runCredCheck(t, `keys := []string{"`+stripeLive(13)+`", "`+awsAccess(14)+`"}`+"\n")
	if len(got.Hits) != 2 || strings.Contains(got.Reason, "column") {
		t.Fatalf("want two hits without columns: %s", got.Reason)
	}
}

func fakePEM(seed int64) string {
	body := fakeToken("MII", 1200, seed)
	var lines []string
	for i := 0; i < len(body); i += 64 {
		end := i + 64
		if end > len(body) {
			end = len(body)
		}
		lines = append(lines, body[i:end])
	}
	return "-----BEGIN " + "PRIVATE KEY-----\\n" + strings.Join(lines, "\\n") + "\\n-----END " + "PRIVATE KEY-----"
}

// TestPrivateKeys_InLargeMinifiedBundle: PEM keys embedded as JSON-style
// strings in a bundle over 1 MiB are found, each with its column.
func TestPrivateKeys_InLargeMinifiedBundle(t *testing.T) {
	root := t.TempDir()
	body := filler(1300000, 15) + `const a="` + fakePEM(15) + `";` + filler(2000, 16) + `const b="` + fakePEM(16) + `";`
	writeBundle(t, root, "web/server.js", body)
	got := NoPrivateKeysInRepo(winCtx(root))
	ms := columnRe.FindAllStringSubmatch(got.Reason, -1)
	if got.Outcome != engine.OutcomeBlock || len(ms) != 2 {
		t.Fatalf("want two keys with columns, got %s: %s", got.Outcome, got.Reason)
	}
}

// TestPrivateKeys_HeaderNearCutKeepsItsBody: a header just before a window
// cut has its body in the next piece; the overlap must still see both.
func TestPrivateKeys_HeaderNearCutKeepsItsBody(t *testing.T) {
	root := t.TempDir()
	body := filler(textWindowBytes-30, 17) + `"` + fakePEM(17) + `"` + filler(100000, 18)
	writeBundle(t, root, "web/x.js", body)
	got := NoPrivateKeysInRepo(winCtx(root))
	if got.Outcome != engine.OutcomeBlock || len(got.Hits) != 1 {
		t.Fatalf("outcome %s: %s", got.Outcome, got.Reason)
	}
}

// TestCredentials_OpenAILegacyNeedsDigits: joined words after "sk-" (as in
// golang.org/x/text's language-name tables) are not a key; a real-shaped key
// with digits still is.
func TestCredentials_OpenAILegacyNeedsDigits(t *testing.T) {
	words := "sk-" + "ArameeSlavaansSamiSamaritaansScholastischSchotsGa"
	got := runCredCheck(t, `names = "Samaritaan`+words+`elic"`+"\n")
	if got.Outcome != engine.OutcomePass {
		t.Fatalf("word run: outcome %s: %s", got.Outcome, got.Reason)
	}
	got = runCredCheck(t, `k = "`+fakeToken("sk-", 46, 20)+`42"`+"\n")
	if got.Outcome != engine.OutcomeBlock || !strings.Contains(got.Reason, "OpenAI API key") {
		t.Fatalf("real-shaped key: outcome %s: %s", got.Outcome, got.Reason)
	}
}
