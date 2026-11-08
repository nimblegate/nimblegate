// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"bytes"
	"fmt"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"

	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
)

const plaintextPwDisableMarker = "appframes:disable security/no-plaintext-passwords"
const plaintextPwDisableLineMarker = "appframes:disable-next-line security/no-plaintext-passwords"

// pwValueRule is one line pattern whose capture group `group` holds the
// password. userGroup, when non-zero, holds the matching user name so a
// `user:user` dev default can be told apart from a real secret.
type pwValueRule struct {
	label     string
	re        *regexp.Regexp
	group     int
	userGroup int
}

// pwCommandRules are shapes where the surrounding command names the value as
// a password. They run on every text file: scripts, CI config, docs, and the
// approved-command lists agents keep (`.claude/settings.local.json`).
var pwCommandRules = []pwValueRule{
	{"mysql password flag", regexp.MustCompile(`\b(?:mysql|mysqldump|mysqladmin|mariadb|mariadb-dump)\b[^\n]*?(?:\s-p([^\s'"]+|'[^']*'|"[^"]*")|\s--password=(['"]?[^\s'"]+))`), 1, 0},
	{"sshpass password", regexp.MustCompile(`\bsshpass\b[^\n]*?\s-p\s*(['"]?[^\s'"]+)`), 1, 0},
	{"curl user:password", regexp.MustCompile(`\bcurl\b[^\n]*?\s(?:-u|--user)(?:\s+|=)['"]?([^\s'":]+):([^\s'"]+)`), 2, 1},
	{"password environment variable", regexp.MustCompile(`\b(?:PGPASSWORD|MYSQL_PWD|SSHPASS|REDISCLI_AUTH)=(['"]?[^\s'";]+)`), 1, 0},
	{"docker login password", regexp.MustCompile(`\bdocker\s+login\b[^\n]*?\s(?:-p|--password)(?:\s+|=)(['"]?[^\s'"]+)`), 1, 0},
	{"redis-cli password", regexp.MustCompile(`\bredis-cli\b[^\n]*?\s-a\s+(['"]?[^\s'"]+)`), 1, 0},
}

// pwURLRe matches userinfo with a password: scheme://user:pass@host.
var pwURLRe = regexp.MustCompile("([A-Za-z][A-Za-z0-9+.\\-]*)://([^\\s:/@'\"`<>]+):([^\\s/@'\"`<>]*)@(\\[[0-9A-Fa-f:]+\\]|[A-Za-z0-9.\\-]+)")

// pwFileRules apply only to files whose name defines the format, so the
// field is known to be a credential.
type pwFileRule struct {
	label string
	match func(rel string) bool
	re    *regexp.Regexp // nil: the whole trimmed file is the secret
	group int
}

var pwFileRules = []pwFileRule{
	{".pgpass entry", baseIs(".pgpass"), regexp.MustCompile(`^\s*[^#\s][^:]*:[^:]*:[^:]*:[^:]*:(.+)$`), 1},
	{".netrc password", baseIs(".netrc", "_netrc"), regexp.MustCompile(`\bpassword\s+(\S+)`), 1},
	{".npmrc auth token", baseIs(".npmrc"), regexp.MustCompile(`(?:_authToken|_auth|_password)\s*=\s*(\S+)`), 1},
	{".pypirc password", baseIs(".pypirc"), regexp.MustCompile(`^\s*password\s*[:=]\s*(\S+)`), 1},
	{"MySQL option file password", baseIs(".my.cnf", "my.cnf", "my.ini"), regexp.MustCompile(`^\s*password\s*=\s*(\S+)`), 1},
	{"Docker registry auth", func(rel string) bool {
		return filepath.Base(rel) == "config.json" && filepath.Base(filepath.Dir(rel)) == ".docker"
	}, regexp.MustCompile(`"auth"\s*:\s*"([^"]+)"`), 1},
	{"Vault token file", baseIs(".vault-token"), nil, 0},
	{"Rails master key", func(rel string) bool {
		rel = filepath.ToSlash(rel)
		return rel == "config/master.key" || strings.HasSuffix(rel, "/config/master.key") ||
			(strings.Contains(rel, "config/credentials/") && strings.HasSuffix(rel, ".key"))
	}, regexp.MustCompile(`^\s*([0-9a-f]{32})\s*$`), 1},
}

func baseIs(names ...string) func(string) bool {
	return func(rel string) bool {
		b := filepath.Base(rel)
		for _, n := range names {
			if b == n {
				return true
			}
		}
		return false
	}
}

// pwPlaceholderWords are values that stand in for a password in docs,
// templates and examples.
var pwPlaceholderWords = map[string]bool{
	"password": true, "passwd": true, "pass": true, "pwd": true, "secret": true,
	"changeme": true, "change_me": true, "changeit": true, "token": true,
	"user": true, "username": true, "mypassword": true, "yourpassword": true,
	"your_password": true, "your-password": true, "none": true, "null": true,
}

// pwConstNameRe matches an UPPER_SNAKE name standing in for a value in docs
// (GITEA_TOKEN, DB_PASSWORD).
var pwConstNameRe = regexp.MustCompile(`^[A-Z][A-Z0-9]*_[A-Z0-9_]+$`)

var pwFakeMarkers = []string{"fake", "dummy", "example", "placeholder", "changeme", "redacted", "xxxx", "sample", "your_", "your-", "***"}

// isPasswordPlaceholder reports whether v is a stand-in rather than a real
// value: empty, an environment or template reference, a placeholder word, a
// marked fake, or the same as the user name (postgres:postgres).
func isPasswordPlaceholder(v, user string) bool {
	v = strings.Trim(strings.TrimSpace(v), `'"`)
	if v == "" {
		return true
	}
	switch v[0] {
	case '$', '%', '<', '{', '[', '(', '*':
		return true
	}
	for _, ref := range []string{"${", "$(", "{{", "%s", "%("} {
		if strings.Contains(v, ref) {
			return true
		}
	}
	lv := strings.ToLower(v)
	if pwPlaceholderWords[lv] {
		return true
	}
	for _, m := range pwFakeMarkers {
		if strings.Contains(lv, m) {
			return true
		}
	}
	if strings.Count(v, v[:1]) == len(v) || pwConstNameRe.MatchString(v) {
		return true
	}
	return user != "" && strings.EqualFold(v, user)
}

// pwLocalHost reports hosts that only exist on a dev machine or in docs, where
// a password in the URL is a local default rather than a leak.
func pwLocalHost(host string) bool {
	h := strings.ToLower(strings.Trim(host, "[]"))
	if i := strings.LastIndex(h, ":"); i > 0 && !strings.Contains(h[:i], ":") {
		h = h[:i]
	}
	switch {
	case h == "localhost", h == "0.0.0.0", h == "::1", h == "host.docker.internal",
		strings.HasPrefix(h, "127."),
		// RFC 5737 documentation ranges.
		strings.HasPrefix(h, "192.0.2."), strings.HasPrefix(h, "198.51.100."), strings.HasPrefix(h, "203.0.113."):
		return true
	}
	for _, suf := range []string{".localhost", ".local", ".test", ".example", ".invalid"} {
		if strings.HasSuffix(h, suf) {
			return true
		}
	}
	for _, d := range []string{"example.com", "example.org", "example.net", "contoso.com", "fabrikam.com"} {
		if h == d || strings.HasSuffix(h, "."+d) {
			return true
		}
	}
	return false
}

// pwLineFinding returns the label of the first plaintext password on line,
// or "".
func pwLineFinding(line string) string {
	for _, m := range pwURLRe.FindAllStringSubmatch(line, -1) {
		if !isPasswordPlaceholder(m[3], m[2]) && !pwLocalHost(m[4]) {
			return "password in URL"
		}
	}
	for _, r := range pwCommandRules {
		for _, m := range r.re.FindAllStringSubmatch(line, -1) {
			v := m[r.group]
			if v == "" && r.group+1 < len(m) {
				v = m[r.group+1]
			}
			user := ""
			if r.userGroup > 0 {
				user = m[r.userGroup]
			}
			if !isPasswordPlaceholder(v, user) {
				return r.label
			}
		}
	}
	return ""
}

// scanFileList returns the files a file-scan frame inspects, following the
// standard scope contract: cli + no ChangedFiles walks the project,
// otherwise ChangedFiles only (empty pre-commit passes).
func scanFileList(ctx engine.CheckContext) []string {
	files := ctx.ChangedFiles
	if len(files) == 0 && ctx.Trigger == engine.TriggerCLI {
		_ = filepath.WalkDir(ctx.ProjectRoot, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if ShouldSkipPath(ctx, path) {
					return filepath.SkipDir
				}
				return nil
			}
			files = append(files, path)
			return nil
		})
	}
	return files
}

// relPath is path relative to the project root, for name-based rules.
func relPath(ctx engine.CheckContext, path string) string {
	if rel, err := filepath.Rel(ctx.ProjectRoot, path); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return path
}

// NoPlaintextPasswords blocks plain passwords in the shapes where context
// proves the value is one: a password in URL userinfo, a password passed to a
// command (mysql -p, sshpass -p, curl -u, PGPASSWORD=, docker login -p,
// redis-cli -a), and credential files whose format names the field (.pgpass,
// .netrc, .npmrc, .pypirc, .my.cnf, Docker config auths, .vault-token, Rails
// master.key). Placeholders, environment references, marked fakes, local
// hosts and user==password dev defaults never fire.
//
// Redaction guarantee: values are NEVER echoed; the reason names the file,
// line and kind of finding only.
func NoPlaintextPasswords(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "security/no-plaintext-passwords",
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
		data, ok := ReadFileBounded(file, DefaultMaxFileBytes)
		if !ok || bytes.IndexByte(data, 0) >= 0 {
			continue
		}
		content := string(data)
		if fileDisabledByMarker(content, plaintextPwDisableMarker) {
			continue
		}
		rel := relPath(ctx, file)
		var fileRules []pwFileRule
		for _, fr := range pwFileRules {
			if fr.match(rel) {
				fileRules = append(fileRules, fr)
			}
		}
		add := func(line int, label string) bool {
			hits = append(hits, fmt.Sprintf("%s:%d - %s", file, line, label))
			hitsStruct = append(hitsStruct, engine.Hit{File: file, Line: line, Label: label})
			return len(hits) >= hitCap
		}
		for _, fr := range fileRules {
			if fr.re == nil && !isPasswordPlaceholder(content, "") {
				if add(1, fr.label) {
					break filesLoop
				}
			}
		}
		// Test code builds credential URLs and commands on purpose to test
		// parsers and redaction; only the credential-file formats apply there.
		lineRules := !isTestOrFixturePath(rel)
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			if i > 0 && lineCarriesMarker(lines[i-1], plaintextPwDisableLineMarker) {
				continue
			}
			label := ""
			for _, fr := range fileRules {
				if fr.re == nil {
					continue
				}
				if m := fr.re.FindStringSubmatch(line); m != nil && !isPasswordPlaceholder(m[fr.group], "") {
					label = fr.label
					break
				}
			}
			if label == "" && lineRules {
				label = pwLineFinding(line)
			}
			if label != "" && add(i+1, label) {
				break filesLoop
			}
		}
	}

	if len(hits) == 0 {
		res.Outcome = engine.OutcomePass
		return res
	}
	res.Hits = hitsStruct
	res.Outcome = engine.OutcomeBlock
	res.Reason = "plaintext password committed (values redacted): " + strings.Join(hits, "; ")
	res.Fix = "remove the password and ROTATE it (assume leaked); pass it through an environment variable or secret manager instead (`$DB_PASSWORD`, `sshpass -e`, `mysql --login-path`, a credential file outside the repo); for agent settings, untrack `.claude/settings.local.json` and keep it in .gitignore; use `# appframes:disable-next-line security/no-plaintext-passwords` only for known-fake fixtures"
	return res
}
