// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"bytes"
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
)

const possibleSecretDisableMarker = "appframes:disable security/possible-secret"
const possibleSecretDisableLineMarker = "appframes:disable-next-line security/possible-secret"

// secretAssignQuotedRe: name = "literal" in code and config (=, :=, :).
var secretAssignQuotedRe = regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_.\-]*)["']?\s*(?::=|=|:)\s*["']([^"'\s]{8,})["']`)

// secretAssignBareRe: name: value / name = value with an unquoted value, only
// in config formats (in code an unquoted right-hand side is an expression).
var secretAssignBareRe = regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_.\-]*)\s*[:=]\s*([^\s"'#;,(){}\[\]]{8,})\s*(?:#.*)?$`)

var secretBareExts = map[string]bool{".yml": true, ".yaml": true, ".ini": true, ".cfg": true, ".conf": true, ".properties": true, ".toml": true}

// secretLiteralRe / secretBearerRe pick out candidate high-entropy strings.
var secretLiteralRe = regexp.MustCompile(`["']([A-Za-z0-9+/=_\-.]{20,})["']`)
var secretBearerRe = regexp.MustCompile(`Bearer\s+([A-Za-z0-9+/=_\-.]{20,})`)

var secretUUIDRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// secretWordPartRe is one part of a separator-joined name (x-aws-ec2-token,
// oauth2.google.tokenSource): letters, optionally followed by digits.
var secretWordPartRe = regexp.MustCompile(`^[A-Za-z]+[0-9]*$`)

var secretConstNameRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]+$`)

// secretContextWords mark a line (or the line above) as talking about a
// credential, which is what lets a random-looking string count.
var secretContextWords = []string{"secret", "token", "password", "passwd", "apikey", "api_key", "api-key", "credential", "bearer", "authorization", "private"}

// secretHashWords mark digests and integrity values, which look random but
// are public.
var secretHashWords = []string{"integrity", "checksum", "digest", "sha1", "sha256", "sha384", "sha512", "hash"}

var secretLastWords = map[string]bool{"password": true, "passwd": true, "pwd": true, "passphrase": true, "secret": true, "token": true, "apikey": true}
var secretLastPairs = map[string]bool{"api key": true, "secret key": true, "access key": true, "private key": true, "auth token": true, "client secret": true, "access token": true, "refresh token": true}

var secretLockfiles = map[string]bool{"package-lock.json": true, "yarn.lock": true, "pnpm-lock.yaml": true, "go.sum": true, "Cargo.lock": true, "poetry.lock": true, "composer.lock": true, "Gemfile.lock": true, "bun.lock": true}

// nameWords splits an identifier into lowercase words on separators and
// camelCase boundaries: dbPassword -> [db password], SECRET_KEY -> [secret key].
func nameWords(name string) []string {
	var words []string
	var cur []rune
	rs := []rune(name)
	flush := func() {
		if len(cur) > 0 {
			words = append(words, strings.ToLower(string(cur)))
			cur = cur[:0]
		}
	}
	for i, r := range rs {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			flush()
			continue
		}
		if i > 0 && unicode.IsUpper(r) && unicode.IsLower(rs[i-1]) {
			flush()
		}
		cur = append(cur, r)
	}
	flush()
	return words
}

// secretName reports whether an identifier names a credential by its last
// word (dbPassword, client_secret) or last two (apiKey, SECRET_KEY). A lone
// `key` or a public/publishable key does not count.
func secretName(name string) bool {
	w := nameWords(name)
	if len(w) == 0 {
		return false
	}
	if secretLastWords[w[len(w)-1]] {
		return true
	}
	return len(w) >= 2 && secretLastPairs[w[len(w)-2]+" "+w[len(w)-1]]
}

// charClasses counts lower, upper, digit and symbol classes in v.
func charClasses(v string) (lower, upper, digit, symbol bool) {
	for _, r := range v {
		switch {
		case unicode.IsLower(r):
			lower = true
		case unicode.IsUpper(r):
			upper = true
		case unicode.IsDigit(r):
			digit = true
		default:
			symbol = true
		}
	}
	return
}

func countTrue(bs ...bool) int {
	n := 0
	for _, b := range bs {
		if b {
			n++
		}
	}
	return n
}

// secretAssignedValue reports whether v looks like a real secret value for a
// credential-named field: not a placeholder or reference, not naming the
// field itself, and mixing at least two character classes.
func secretAssignedValue(v string) bool {
	if isPasswordPlaceholder(v, "") || secretConstNameRe.MatchString(v) {
		return false
	}
	lv := strings.ToLower(v)
	for _, ref := range []string{"env.", "env[", "getenv", "environ", "secrets.", "vault:", "password", "secret", "token", "confirm"} {
		if strings.Contains(lv, ref) {
			return false
		}
	}
	// A digit or capital is required: lowercase words joined by hyphens
	// (same-origin, bert-base-uncased) are option values, not secrets.
	l, u, d, s := charClasses(v)
	return (u || d) && countTrue(l, u, d, s) >= 2
}

// shannon returns the entropy of v in bits per character.
func shannon(v string) float64 {
	freq := map[rune]int{}
	for _, r := range v {
		freq[r]++
	}
	n := float64(len([]rune(v)))
	h := 0.0
	for _, c := range freq {
		p := float64(c) / n
		h -= p * math.Log2(p)
	}
	return h
}

// highEntropyValue: lower, upper and digits all present (rules out hex
// hashes and UUIDs) and at least 3.5 bits per character.
// hasContextWord finds a secret keyword as a word or the end of an
// identifier (authToken, api_key) but not inside a longer word (tokenizer,
// AutoTokenizer, secretary).
func hasContextWord(ll string) bool {
	for _, w := range secretContextWords {
		for i := 0; ; {
			j := strings.Index(ll[i:], w)
			if j < 0 {
				break
			}
			end := i + j + len(w)
			if end < len(ll) && ll[end] == 's' {
				end++
			}
			if end >= len(ll) || !unicode.IsLetter(rune(ll[end])) {
				return true
			}
			i = i + j + 1
		}
	}
	return false
}

// lowerRunShare is the share of v's characters that sit in runs of three or
// more lowercase letters.
func lowerRunShare(v string) float64 {
	in, run := 0, 0
	for _, r := range v + "." {
		if r >= 'a' && r <= 'z' {
			run++
			continue
		}
		if run >= 3 {
			in += run
		}
		run = 0
	}
	return float64(in) / float64(len(v))
}

// secretAlphabetRuns mark a character set (token generators, base64
// alphabets), not a secret.
var secretAlphabetRuns = []string{"abcdefghij", "ABCDEFGHIJ", "0123456789"}

func highEntropyValue(v string) bool {
	for _, run := range secretAlphabetRuns {
		if strings.Contains(v, run) {
			return false
		}
	}
	if secretUUIDRe.MatchString(v) || isPasswordPlaceholder(v, "") {
		return false
	}
	for _, p := range []string{"sha1-", "sha256-", "sha384-", "sha512-", "data:"} {
		if strings.HasPrefix(v, p) {
			return false
		}
	}
	// Names joined by separators look random to an entropy count but are
	// header names, config keys and dotted identifiers.
	if parts := strings.FieldsFunc(v, func(r rune) bool { return r == '-' || r == '_' || r == '.' }); len(parts) >= 2 {
		words := true
		for _, p := range parts {
			if !secretWordPartRe.MatchString(p) {
				words = false
				break
			}
		}
		if words {
			return false
		}
	}
	// CamelCase type and field names (GoogleCloudV1Oauth2ClientConfig) are
	// mostly runs of lowercase letters; random strings almost never are.
	if lowerRunShare(v) >= 0.5 {
		return false
	}
	// Random strings of this length nearly always carry several digits and
	// capitals; CamelCase identifiers (QueryServiceConfig2W) rarely do.
	digits, uppers := 0, 0
	for _, r := range v {
		switch {
		case unicode.IsDigit(r):
			digits++
		case unicode.IsUpper(r):
			uppers++
		}
	}
	l, _, _, _ := charClasses(v)
	return l && digits >= 2 && uppers >= 2 && shannon(v) >= 3.5
}

func containsAny(s string, words []string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

// possibleSecretSkipFile: tests and fixtures carry fake secrets by design;
// lockfiles and minified bundles are full of random-looking public strings.
func possibleSecretSkipFile(rel string) bool {
	base := filepath.Base(rel)
	if secretLockfiles[base] || strings.HasSuffix(base, ".min.js") || strings.HasSuffix(base, ".min.css") || strings.HasSuffix(base, ".map") {
		return true
	}
	return isTestOrFixturePath(rel) || isThirdPartyPath(rel)
}

// isThirdPartyPath reports vendored dependencies, virtualenvs and framework
// build caches: their docs and tests carry example credentials, and none of it
// holds the project's own secrets.
func isThirdPartyPath(rel string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(rel), "/") {
		switch seg {
		case ".venv", "venv", "site-packages", "vendor", "third_party", "bower_components", ".svelte-kit":
			return true
		}
	}
	return false
}

// isTestOrFixturePath reports test sources and fixture directories, which
// carry fake credentials by design. Rust keeps tests inline in ordinary
// source files, so .rs files are not treated as tests.
func isTestOrFixturePath(rel string) bool {
	if k := testFileKind(rel); k != "" && k != "rs" {
		return true
	}
	if inTestDir(rel) {
		return true
	}
	for _, seg := range strings.Split(filepath.ToSlash(filepath.Dir(rel)), "/") {
		switch seg {
		case "testdata", "fixtures", "__fixtures__", "__mocks__":
			return true
		}
	}
	return false
}

// possibleSecretLine returns the finding label for one line, or "".
func possibleSecretLine(line, prev string, bare bool) string {
	for _, p := range credentialPatterns {
		if p.Severity == engine.OutcomeBlock && credentialLineFires(p, line) {
			return "" // a known-prefix key: no-hardcoded-credentials owns it
		}
	}
	for _, m := range secretAssignQuotedRe.FindAllStringSubmatch(line, -1) {
		if secretName(m[1]) && secretAssignedValue(m[2]) {
			return "possible secret in " + m[1]
		}
	}
	if bare {
		if m := secretAssignBareRe.FindStringSubmatch(line); m != nil && secretName(m[1]) && secretAssignedValue(m[2]) {
			return "possible secret in " + m[1]
		}
	}
	ll := strings.ToLower(line)
	if containsAny(ll, secretHashWords) {
		return ""
	}
	// Minified bundles are one long line of random-looking identifiers.
	if len(line) > 500 {
		return ""
	}
	if !hasContextWord(ll) && !hasContextWord(strings.ToLower(prev)) {
		return ""
	}
	var cands []string
	for _, m := range secretLiteralRe.FindAllStringSubmatch(line, -1) {
		cands = append(cands, m[1])
	}
	for _, m := range secretBearerRe.FindAllStringSubmatch(line, -1) {
		cands = append(cands, m[1])
	}
	for _, v := range cands {
		if highEntropyValue(v) {
			return "high-entropy string near a secret keyword"
		}
	}
	return ""
}

// PossibleSecret warns about values that are probably secrets but cannot be
// proven to be: a credential-named field (dbPassword, SECRET_KEY, apiKey,
// client_secret) holding a real-looking literal, and a high-entropy string on
// or just below a line that talks about a secret, token or authorization.
// WARN only: a guess never blocks a push. Tests, fixtures, lockfiles and
// minified files are skipped; known-prefix keys are left to
// no-hardcoded-credentials, which blocks them.
//
// Redaction guarantee: values are NEVER echoed; the reason names the file,
// line and field only.
func PossibleSecret(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "security/possible-secret",
		Category: frames.CategorySecurity,
	}
	var hits []string
	var hitsStruct []engine.Hit
	const hitCap = 10

filesLoop:
	for _, file := range scanFileList(ctx) {
		if ShouldSkipPath(ctx, file) {
			continue
		}
		rel := relPath(ctx, file)
		if possibleSecretSkipFile(rel) {
			continue
		}
		data, ok := ReadFileBounded(file, DefaultMaxFileBytes)
		if !ok || bytes.IndexByte(data, 0) >= 0 {
			continue
		}
		content := string(data)
		if fileDisabledByMarker(content, possibleSecretDisableMarker) {
			continue
		}
		bare := secretBareExts[strings.ToLower(filepath.Ext(file))]
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			prev := ""
			if i > 0 {
				prev = lines[i-1]
				if lineCarriesMarker(prev, possibleSecretDisableLineMarker) {
					continue
				}
			}
			if label := possibleSecretLine(line, prev, bare); label != "" {
				hits = append(hits, fmt.Sprintf("%s:%d - %s", file, i+1, label))
				hitsStruct = append(hitsStruct, engine.Hit{File: file, Line: i + 1, Label: label})
				if len(hits) >= hitCap {
					break filesLoop
				}
			}
		}
	}

	if len(hits) == 0 {
		res.Outcome = engine.OutcomePass
		return res
	}
	res.Hits = hitsStruct
	res.Outcome = engine.OutcomeWarn
	res.Reason = "possible secrets (values redacted; check each - a guess, not proof): " + strings.Join(hits, "; ")
	res.Fix = "if the value is real, move it to an environment variable or secret manager and ROTATE it; if it is a fake or public value, mark it with `# appframes:disable-next-line security/possible-secret`"
	return res
}
