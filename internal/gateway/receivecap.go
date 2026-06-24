// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"fmt"
	"os/exec"
	"regexp"
)

// DefaultReceiveMaxInputSize is the receive.maxInputSize value applied to
// every newly-registered bare repo. 500 MiB is generous for code repositories
// (the typical legitimate push is a few MB), small enough to cap a single
// attacker's disk-fill impact on the gateway box, and compatible with most
// upstream hosts' limits (GitHub 2 GiB, GitLab 5 GiB default, Gitea unlimited
// - 500m fits inside all of them).
//
// Repos with legitimate large binaries should turn on Git LFS at the upstream
// rather than raise this cap; LFS bypasses git's pack mechanism so the
// gateway only receives small pointer files regardless of binary size.
// (Note that LFS uploads bypass the gateway entirely - see
// docs/server/SECURITY-MODEL.md "Git LFS interaction" for the trade-off.)
const DefaultReceiveMaxInputSize = "500m"

// receiveCapPattern validates the size string format git's config parser
// accepts: an integer followed by an optional k/m/g suffix (case-insensitive),
// or "0" / empty for "unlimited."
var receiveCapPattern = regexp.MustCompile(`^(0|\d+[kKmMgG]?)$`)

// ValidateReceiveCap returns nil if size matches git's config-size format.
// Empty string is valid (means "unlimited" / "unset"). Invalid → error
// describing the expected format.
func ValidateReceiveCap(size string) error {
	if size == "" {
		return nil
	}
	if !receiveCapPattern.MatchString(size) {
		return fmt.Errorf("invalid size %q: expected NNN (bytes) or NNNk / NNNm / NNNg (kilo / mega / gigabytes), e.g. 500m", size)
	}
	return nil
}

// ApplyReceiveCap runs `git config receive.maxInputSize <size>` against the
// bare repo at repoPath. Caller must have validated size via ValidateReceiveCap.
// Empty size → unsets the config (git treats absent as unlimited).
func ApplyReceiveCap(repoPath, size string) error {
	if size == "" {
		// Unset the config - explicit "no limit" state.
		out, err := exec.Command("git", "-C", repoPath, "config", "--unset", "receive.maxInputSize").CombinedOutput()
		if err != nil {
			// git config --unset returns 5 if the key wasn't set; that's
			// equivalent to "already unset," not an error.
			if exitCode(err) == 5 {
				return nil
			}
			return fmt.Errorf("git config --unset receive.maxInputSize: %w\n%s", err, out)
		}
		return nil
	}
	if err := ValidateReceiveCap(size); err != nil {
		return err
	}
	out, err := exec.Command("git", "-C", repoPath, "config", "receive.maxInputSize", size).CombinedOutput()
	if err != nil {
		return fmt.Errorf("git config receive.maxInputSize=%s: %w\n%s", size, err, out)
	}
	return nil
}

// exitCode extracts the integer exit code from an exec error. Returns -1 if
// the error isn't an ExitError (e.g., the command couldn't even start).
func exitCode(err error) int {
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return -1
}
