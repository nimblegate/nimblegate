// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package version

import "runtime/debug"

// Version is the nimblegate CLI version, intended to be set at build time via
// -ldflags "-X nimblegate/internal/version.Version=<sha>". When it's left at the
// default, Resolved() falls back to the VCS commit the Go toolchain embeds, so
// even a plain `go build` reports the real revision instead of "0.0.0-dev".
var Version = "0.0.0-dev"

// Resolved returns the version string to display: the -ldflags override if one
// was given, otherwise the VCS revision baked into the binary by `go build`
// (short, with a "-dirty" suffix when the worktree had uncommitted changes),
// otherwise the "0.0.0-dev" default.
func Resolved() string {
	rev, modified := vcsInfo()
	return resolve(Version, rev, modified)
}

// resolve is the pure precedence decision, split out from vcsInfo so it can be
// tested deterministically: explicit ldflag override wins; else the VCS
// revision (shortened, +"-dirty"); else the ldflag default.
func resolve(ldflag, rev string, modified bool) string {
	if ldflag != "" && ldflag != "0.0.0-dev" {
		return ldflag
	}
	if rev != "" {
		if len(rev) > 7 {
			rev = rev[:7]
		}
		if modified {
			rev += "-dirty"
		}
		return rev
	}
	return ldflag
}

// vcsInfo reads the VCS revision and dirty flag the Go toolchain embeds into the
// binary at build time (available for any `go build` inside a VCS repo unless
// -buildvcs=false). Empty when unavailable (e.g. `go run`, built outside a repo).
func vcsInfo() (rev string, modified bool) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "", false
	}
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	return rev, modified
}
