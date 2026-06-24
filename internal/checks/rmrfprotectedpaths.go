// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"nimblegate/internal/canonical"
	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
)

// defaultProtectedPrefixPaths are paths protected as PREFIXES -
// `rm -rf /etc/anything` blocks because of /etc. These are OS-owned
// trees where rm -rf almost always indicates a mistake.
var defaultProtectedPrefixPaths = []string{
	"/",
	"/bin",
	"/boot",
	"/dev",
	"/etc",
	"/lib",
	"/lib32",
	"/lib64",
	"/opt",
	"/proc",
	"/root",
	"/run",
	"/sbin",
	"/srv",
	"/sys",
	"/usr",
	"/var",
}

// defaultProtectedExactPaths are protected only at EXACT match.
// `rm -rf /home` blocks (would wipe every user's home), but
// `rm -rf /home/alice/project/build` passes because individual user
// homes are legitimate working space.
var defaultProtectedExactPaths = []string{
	"/home",
}

// recursiveFlags matches a single recursive-rm flag (the args list is
// checked entry-by-entry, so combined short flags like -rf decompose
// elsewhere - see hasRecursiveFlag).
var recursiveFlagSet = map[string]bool{
	"-r":          true,
	"-R":          true,
	"--recursive": true,
}

// unexpandedVarRegex catches argument forms whose shell-variable
// expansion would resolve to empty, producing a catastrophic rm-rf
// target like /. Patterns we flag:
//
//	$VAR/       - unset variable expands to "" then has "/"
//	${VAR}/
//	""/         - literal empty-quoted prefix
//	''/
//
// The check fires when ctx.Command - the command-line as we received
// it from the shell wrapper - contains one of these BEFORE shell
// expansion. (We never see the post-expansion form; if the shell
// already expanded an empty var to nothing, the target reads as bare
// "/" and the protected-path check catches it.)
var unexpandedEmptyRoot = regexp.MustCompile(`(?:\$[A-Za-z_][A-Za-z_0-9]*|\$\{[A-Za-z_][A-Za-z_0-9]*\}|""|'')/`)

// RmRfProtectedPaths blocks recursive `rm` invocations targeting
// catastrophic paths. See internal/stdlib/frames/filesystem/rm-rf-protected-paths.md.
//
// Detection runs against ctx.Command (string from the git-wrap shell
// wrapper). Per-file disables don't apply; override via --force-yes
// at the wrapper level.
func RmRfProtectedPaths(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "filesystem/rm-rf-protected-paths",
		Category: frames.CategoryFilesystem,
	}

	args := strings.Fields(ctx.Command)
	if len(args) < 2 || args[0] != "rm" {
		res.Outcome = engine.OutcomeSkip
		return res
	}
	if !hasRecursiveFlag(args[1:]) {
		// Non-recursive rm - out of scope for this frame.
		res.Outcome = engine.OutcomeSkip
		return res
	}

	// Build the catalog: defaults + per-project additions. Two-tier:
	// prefix-protected (OS trees) vs exact-protected (parents of normal
	// user working space).
	prefixCatalog, exactCatalog := protectedCatalogs(ctx.ProjectRoot)

	// Unexpanded-variable scan - operates on the WHOLE command string so
	// we see things shell-fields stripped: `rm -rf "$ROOT/"*` for example.
	if loc := unexpandedEmptyRoot.FindStringIndex(ctx.Command); loc != nil {
		match := ctx.Command[loc[0]:loc[1]]
		res.Outcome = engine.OutcomeBlock
		res.Reason = fmt.Sprintf("rm -rf with unexpanded/empty variable expansion %q - if the variable is unset this resolves to a top-level deletion",
			match)
		res.Fix = "ensure the variable is set (set -u; ${VAR:?must be set}; default value); re-run; or `nimblegate cmd --force-yes --reason=\"...\" rm -rf ...` to record an audited bypass"
		return res
	}

	// Per-argument protected-path scan.
	for _, raw := range args[1:] {
		if strings.HasPrefix(raw, "-") {
			continue // flag
		}
		arg := stripQuotes(raw)
		// Home-directory shortcuts.
		if arg == "$HOME" || arg == "~" || arg == "~/" {
			res.Outcome = engine.OutcomeBlock
			res.Reason = fmt.Sprintf("rm -rf would target home directory (%s)", arg)
			res.Fix = "you probably don't mean to delete your entire home; `nimblegate cmd --force-yes --reason=\"...\" rm -rf ...` if you do"
			return res
		}
		// Exact-only catalog: BLOCK if equal, but DON'T block prefixes
		// (e.g. /home blocks but /home/alice/work passes).
		for path, reason := range exactCatalog {
			if arg == path {
				res.Outcome = engine.OutcomeBlock
				res.Reason = fmt.Sprintf("rm -rf would target protected path %q (%s)", arg, reason)
				res.Fix = fmt.Sprintf("if you really mean it, `nimblegate cmd --force-yes --reason=\"...\" rm -rf %s` to record an audited bypass", arg)
				return res
			}
		}
		// Prefix catalog: BLOCK if equal OR descendant. When several entries
		// match (e.g. the default "/srv" AND a project's
		// "/srv/projects/critical-data"), the most specific - longest - path
		// wins, so the operator sees the narrowest, most informative reason.
		// Map iteration order is nondeterministic, so the winner MUST be
		// chosen by length rather than by whichever the range happens to hit
		// first.
		bestPath, bestReason := "", ""
		for path, reason := range prefixCatalog {
			if arg == path || strings.HasPrefix(arg, path+"/") {
				if len(path) > len(bestPath) {
					bestPath, bestReason = path, reason
				}
			}
		}
		if bestPath != "" {
			res.Outcome = engine.OutcomeBlock
			res.Reason = fmt.Sprintf("rm -rf would target protected path %q (%s)", arg, bestReason)
			res.Fix = fmt.Sprintf("if you really mean it, `nimblegate cmd --force-yes --reason=\"...\" rm -rf %s` to record an audited bypass", arg)
			return res
		}
	}

	res.Outcome = engine.OutcomePass
	return res
}

// hasRecursiveFlag scans the post-rm argument list for any flag that
// makes rm descend into directories. Includes the common combined
// short flags (-rf, -fr, -Rf, -fR) so we don't miss them.
func hasRecursiveFlag(args []string) bool {
	for _, a := range args {
		if recursiveFlagSet[a] {
			return true
		}
		// Combined short-flag forms: -rf, -fr, -Rf, -fR, -rfv, etc.
		// Any single-dash arg with no '=' that contains 'r' or 'R' counts.
		if strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && !strings.Contains(a, "=") {
			for _, c := range a[1:] {
				if c == 'r' || c == 'R' {
					return true
				}
			}
		}
	}
	return false
}

// stripQuotes trims one matching pair of surrounding quotes (' or ").
// The shell wrapper passes args after its own quote handling, but
// embedded quotes in pathological inputs (e.g. paste from a wiki) can
// still appear.
func stripQuotes(s string) string {
	if len(s) >= 2 {
		first, last := s[0], s[len(s)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// protectedCatalogs returns the merged default+project maps for both
// prefix-protected and exact-protected paths. Project entries from
// .appframes/_canonical/protected-paths.toml join the prefix catalog by
// default (the common case: a project root that should never be wiped
// recursively).
func protectedCatalogs(projectRoot string) (prefix, exact map[string]string) {
	prefix = make(map[string]string, len(defaultProtectedPrefixPaths)+4)
	exact = make(map[string]string, len(defaultProtectedExactPaths))
	for _, p := range defaultProtectedPrefixPaths {
		prefix[p] = defaultPathReason(p)
	}
	for _, p := range defaultProtectedExactPaths {
		exact[p] = defaultPathReason(p)
	}
	if projectRoot == "" {
		return
	}
	tablePath := filepath.Join(projectRoot, ".appframes", "_canonical", "protected-paths.toml")
	if _, err := os.Stat(tablePath); errors.Is(err, fs.ErrNotExist) {
		return
	}
	tbl, err := canonical.Load(tablePath)
	if err != nil {
		return
	}
	if section, ok := tbl.Section("paths"); ok {
		for path, reason := range section {
			prefix[path] = reason
		}
	}
	// Optional: project authors can opt a path into exact-only via
	// [exact-paths] in the same canonical table.
	if section, ok := tbl.Section("exact-paths"); ok {
		for path, reason := range section {
			exact[path] = reason
		}
	}
	return
}

// defaultPathReason maps a built-in protected path to a one-liner
// describing why it's protected.
func defaultPathReason(p string) string {
	switch p {
	case "/":
		return "filesystem root"
	case "/etc":
		return "OS config tree"
	case "/usr", "/usr/local":
		return "shared system binaries + libs"
	case "/var":
		return "system state (logs, package metadata, databases)"
	case "/home":
		return "user homes parent dir"
	case "/root":
		return "root user home"
	case "/boot":
		return "boot loader + kernel"
	case "/sys", "/proc":
		return "kernel pseudo-filesystem"
	case "/dev":
		return "device nodes"
	case "/bin", "/sbin", "/lib", "/lib32", "/lib64":
		return "core OS binaries / libs"
	case "/opt", "/srv", "/run":
		return "system-level state"
	}
	return "protected path"
}
