// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
)

// multiEnvCLIs is the list of CLIs whose invocations need an explicit env
// argument to avoid the "silent local default" footgun. Order doesn't
// matter - the regex matches any of these as the leading word.
var multiEnvCLIs = []string{
	"wrangler", // Cloudflare D1 / KV / R2 / Pages - defaults to LOCAL when --remote omitted
	"gcloud",   // any gcloud invocation without --project picks the active config
	"kubectl",  // current context determines target cluster
	"vercel",   // --prod is opt-in
	"flyctl",   // current org / app inferred
	"fly",      // alias for flyctl
	"supabase", // env-scoped CLI
	"firebase", // env-scoped CLI
	"heroku",   // --app inferred from current dir's remote
}

// envCLIRegex matches an invocation of any multi-env CLI. Used to find the
// command in script lines.
var envCLIRegex = regexp.MustCompile(`(?m)^\s*(?:sudo\s+)?(wrangler|gcloud|kubectl|vercel|flyctl|fly|supabase|firebase|heroku)\b`)

// defaultedScopeVarRegex matches the bash pattern that introduces the
// footgun: a variable assigned from `${1:-...}` or `$1` (which is empty
// when the caller forgot to pass a positional arg). Examples:
//
//	SCOPE="${1:-}"
//	ENV=${1:-}
//	TARGET="$1"     # NOT covered - bash makes $1 empty if unset, not an error
//
// We cover the first two shapes (explicit `${1:-...}` default). The third
// shape (`$1` without default) is also a footgun but very common in shell
// scripts that DO require args; flagging it would produce too many false
// positives.
var defaultedScopeVarRegex = regexp.MustCompile(`^\s*([A-Z_][A-Z0-9_]*)=(?:"\$\{1:-[^}]*\}"|\$\{1:-[^}]*\})`)

// envFlagPresentRegex looks for an explicit --remote / --env / --project
// / --context flag inside the line containing the CLI invocation. When
// present, we ASSUME the script handles env correctly and don't flag.
var envFlagPresentRegex = regexp.MustCompile(`--(?:remote|local|env|environment|project|context|account|app|namespace|stage)\b`)

// scopeValidatedVarRegex captures a variable that is COMPARED against an
// env-scope flag literal - i.e. the script validates it before use:
//
//	[[ "$SCOPE" != "--local" && "$SCOPE" != "--remote" ]]
//	[ "$ENV" = "--remote" ]
//	case "$SCOPE" in --local|--remote)
//
// A variable validated this way is an explicit env flag (passed indirectly),
// so a CLI line that uses it is handled - not the footgun. We capture both
// var-first (`$SCOPE != --local`) and flag-first (`--local == $SCOPE`) shapes.
var scopeValidatedVarRegex = regexp.MustCompile(`\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?"?\s*(?:==|!=|=)\s*"?--(?:remote|local|env|environment|project|context|account|app|namespace|stage)\b`)
var scopeValidatedVarRegexRev = regexp.MustCompile(`--(?:remote|local|env|environment|project|context|account|app|namespace|stage)"?\s*(?:==|!=|=)\s*"?\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?`)

// wranglerFootgunSubcommands are the only wrangler subcommand groups that
// have a local-vs-remote default - the actual footgun this frame targets.
// `wrangler pages deploy`, `wrangler deploy` (Workers), `versions`, `tail`,
// etc. always act on the remote and have no local mode, so an "explicit env
// flag" is meaningless for them. Restricting wrangler to these subcommands
// removes false positives on deploy scripts.
var wranglerFootgunSubcommands = map[string]bool{"d1": true, "kv": true, "r2": true}

// wranglerSubcommandIsFootgun reports whether a `wrangler ...` line invokes a
// data-plane subcommand (d1/kv/r2). The subcommand is the first non-flag word
// after `wrangler`. Returns false for deploy/pages/etc.
func wranglerSubcommandIsFootgun(line string) bool {
	fields := strings.Fields(line)
	seen := false
	for _, f := range fields {
		if !seen {
			if f == "wrangler" {
				seen = true
			}
			continue
		}
		if strings.HasPrefix(f, "-") {
			continue // skip global flags (their values may follow, but the
			// data-plane subcommands never sit behind a flag value in practice)
		}
		return wranglerFootgunSubcommands[f]
	}
	return false
}

// lineUsesAnyVar reports whether the line references any of the given bash
// variables via $VAR or ${VAR} (with a word boundary so $SCOPE doesn't match
// $SCOPED).
func lineUsesAnyVar(line string, vars map[string]bool) bool {
	for v := range vars {
		if strings.Contains(line, "${"+v+"}") {
			return true
		}
		needle := "$" + v
		from := 0
		for {
			j := strings.Index(line[from:], needle)
			if j < 0 {
				break
			}
			end := from + j + len(needle)
			if end >= len(line) || !isWordByte(line[end]) {
				return true
			}
			from += j + 1
		}
	}
	return false
}

func isWordByte(b byte) bool {
	return b == '_' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func migrationScriptApplicableFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".sh", ".bash", ".zsh", ".ksh":
		return true
	}
	// Also catch executable scripts named apply-*-migration.* (no extension).
	base := strings.ToLower(filepath.Base(path))
	if strings.HasPrefix(base, "apply-") && strings.Contains(base, "migration") {
		return true
	}
	return false
}

const migrationScriptDisableMarker = "appframes:disable database/migration-script-explicit-env"
const migrationScriptDisableLineMarker = "appframes:disable-next-line database/migration-script-explicit-env"
const migrationScriptMaxFileBytes = 1 << 20 // 1 MiB

// MigrationScriptExplicitEnv scans bash scripts for the footgun pattern
// where a multi-env CLI (wrangler, gcloud, kubectl, etc.) is invoked
// with an env scope that defaults to empty when the caller forgets to
// pass it. Default-empty resolves to the CLI's local context - which is
// almost always not what production deployment wants.
//
// The detection is conservative: we flag only when ALL three are present
// in the file:
//
//  1. A `${1:-...}` defaulted variable assignment (the footgun shape)
//  2. A multi-env CLI invocation (wrangler, gcloud, kubectl, vercel, ...)
//  3. No explicit env flag on the CLI invocation line (--remote, --env,
//     --project, --context, --account, --app, --namespace, --stage)
//
// This avoids false positives on scripts that handle the env correctly
// or that don't use a multi-env CLI at all.
//
// Reference incident: a pair of apply-migration shell scripts on
// 2026-05-18. Defaulted SCOPE="" → wrangler without --remote treated
// as local → production DDL never applied. Hours of debug + dirty
// prod data.
func MigrationScriptExplicitEnv(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "database/migration-script-explicit-env",
		Category: frames.CategoryDatabase,
	}

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
			if migrationScriptApplicableFile(path) {
				files = append(files, path)
			}
			return nil
		})
	}

	var hits []string
	var hitsStruct []engine.Hit
	const hitCap = 10

filesLoop:
	for _, file := range files {
		if !migrationScriptApplicableFile(file) {
			continue
		}
		if ShouldSkipPath(ctx, file) {
			continue
		}
		info, err := os.Stat(file)
		if err != nil || info.Size() > migrationScriptMaxFileBytes {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		content := string(data)
		if strings.Contains(content, migrationScriptDisableMarker) {
			continue
		}

		// Two-pass: find defaulted vars first, then look for unguarded CLI
		// invocations that USE those vars (or simply invoke without an
		// explicit env flag). Either pattern alone is a footgun.
		lines := strings.Split(content, "\n")
		var defaultedVars []string
		validatedVars := map[string]bool{}
		for _, line := range lines {
			if m := defaultedScopeVarRegex.FindStringSubmatch(line); m != nil {
				defaultedVars = append(defaultedVars, m[1])
			}
			if m := scopeValidatedVarRegex.FindStringSubmatch(line); m != nil {
				validatedVars[m[1]] = true
			}
			if m := scopeValidatedVarRegexRev.FindStringSubmatch(line); m != nil {
				validatedVars[m[1]] = true
			}
		}

		for i, line := range lines {
			if i > 0 && strings.Contains(lines[i-1], migrationScriptDisableLineMarker) {
				continue
			}
			cli := envCLIRegex.FindStringSubmatch(line)
			if cli == nil {
				continue
			}
			// wrangler only carries the local-vs-remote footgun on its
			// data-plane subcommands (d1/kv/r2). Deploy commands (pages
			// deploy, worker deploy) are always remote - skip them.
			if cli[1] == "wrangler" && !wranglerSubcommandIsFootgun(line) {
				continue
			}
			// CLI invocation found. If the SAME line carries an explicit env
			// flag, treat the script as handled.
			if envFlagPresentRegex.MatchString(line) {
				continue
			}
			// Or if it passes a scope variable the script validates against
			// env-flag literals (e.g. `$SCOPE` guarded to --local/--remote),
			// that IS an explicit flag, passed indirectly - handled.
			if lineUsesAnyVar(line, validatedVars) {
				continue
			}
			// Either we saw a defaulted env variable earlier in the file,
			// OR the invocation has no env flag at all - either way, fire.
			var label string
			if len(defaultedVars) > 0 {
				label = fmt.Sprintf("%s invocation without explicit env flag; defaulted var(s) present: %s",
					cli[1], strings.Join(defaultedVars, ", "))
			} else {
				label = fmt.Sprintf("%s invocation without explicit env flag (--remote/--env/--project/--context)", cli[1])
			}
			hits = append(hits, fmt.Sprintf("%s:%d - %s", file, i+1, label))
			hitsStruct = append(hitsStruct, engine.Hit{File: file, Line: i + 1, Label: label})
			if len(hits) >= hitCap {
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
	res.Reason = "multi-env CLI invocations missing an explicit env scope: " + strings.Join(hits, "; ")
	res.Fix = "make the env arg required (not defaulted) AND pass it as an explicit flag - e.g. `wrangler d1 execute \"$DB\" --remote` or `gcloud --project=$PROJECT`. Without it, the CLI silently defaults to the local / current-context env, which is almost never what production wants."
	return res
}
