// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

// ScanTmpDirName is the directory under the repos root that holds per-push
// materialize worktrees when no scan_tmpdir is configured. The default matters:
// os.MkdirTemp("") follows $TMPDIR, which is tmpfs on most distros, so the gate
// would stage a full copy of every pushed tree in RAM - one per in-flight push.
// A disk-backed dir next to the bare repos sizes with the repos themselves.
const ScanTmpDirName = "_scan-tmp"

// reposInternalsDir is the subdirectory the canonical layout keeps the real
// bare repos in (<repos-root>/_repos/<name>.git, symlinked from
// <repos-root>/<name>.git).
const reposInternalsDir = "_repos"

// DefaultMaxTreeBytes caps what one push may expand to on disk. receive.maxInputSize
// bounds the PACK, which says nothing about the tree: a file of 10 GB of zeros is a
// tiny object that extracts in full. 2 GiB is far above any code repo and far below
// what it takes to wedge a gateway disk.
const DefaultMaxTreeBytes int64 = 2 << 30

// DefaultScanTimeout bounds one push's frame run. Every enabled frame walks the
// staged tree itself, so cost grows with repo size and frame count; without a
// deadline a pathological push holds the pusher, and a receive-pack slot, forever.
const DefaultScanTimeout = 5 * time.Minute

// GatewayConfig is the [gateway] section of <policy-root>/gateway.toml: the
// machine-level resource limits, as opposed to the per-repo policy in
// <policy-root>/<repo>/gateway.toml.
type GatewayConfig struct {
	ScanTmpDir   string        // "" = <repos-root>/_scan-tmp
	MaxTreeBytes int64         // 0 = unlimited
	ScanTimeout  time.Duration // 0 = no deadline
}

// DefaultGatewayConfig is what an operator gets with no [gateway] section.
func DefaultGatewayConfig() GatewayConfig {
	return GatewayConfig{MaxTreeBytes: DefaultMaxTreeBytes, ScanTimeout: DefaultScanTimeout}
}

// gatewayTOML mirrors only the [gateway] section of <policy-root>/gateway.toml,
// so unrelated sections (maintenance, future features) decode without error.
// Pointers distinguish "absent" (take the default) from "set to zero"
// (explicitly unlimited).
type gatewayTOML struct {
	Gateway struct {
		ScanTmpDir   *string `toml:"scan_tmpdir"`
		MaxTreeBytes *int64  `toml:"max_tree_bytes"`
		ScanTimeout  *string `toml:"scan_timeout"`
	} `toml:"gateway"`
}

// LoadGatewayConfig reads the [gateway] section of <policy-root>/gateway.toml.
// A missing file or absent section yields DefaultGatewayConfig with no error;
// a malformed file or value returns the error, so an operator typo surfaces
// rather than silently reverting to a default they did not ask for.
func LoadGatewayConfig(policyRoot string) (GatewayConfig, error) {
	cfg := DefaultGatewayConfig()
	path := filepath.Join(policyRoot, "gateway.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("gateway: read %s: %w", path, err)
	}
	var raw gatewayTOML
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return cfg, fmt.Errorf("gateway: parse %s: %w", path, err)
	}
	if raw.Gateway.ScanTmpDir != nil {
		cfg.ScanTmpDir = *raw.Gateway.ScanTmpDir
	}
	if raw.Gateway.MaxTreeBytes != nil {
		if *raw.Gateway.MaxTreeBytes < 0 {
			return cfg, fmt.Errorf("gateway: max_tree_bytes must be >= 0 (0 = unlimited); got %d", *raw.Gateway.MaxTreeBytes)
		}
		cfg.MaxTreeBytes = *raw.Gateway.MaxTreeBytes
	}
	if raw.Gateway.ScanTimeout != nil {
		d, err := time.ParseDuration(*raw.Gateway.ScanTimeout)
		if err != nil {
			return cfg, fmt.Errorf("gateway: scan_timeout: %w", err)
		}
		if d < 0 {
			return cfg, fmt.Errorf("gateway: scan_timeout must be >= 0 (0 = no deadline); got %s", d)
		}
		cfg.ScanTimeout = d
	}
	return cfg, nil
}

// ScanStagingDir picks the staging dir for a full-tree materialization out of
// a bare repo, honoring [gateway] scan_tmpdir when the caller knows the policy
// root (pass "" when it does not). Every gateway-side path that extracts a
// whole tree goes through here, so the maintenance sweeper has exactly one
// directory to clean.
func ScanStagingDir(bareDir, policyRoot string) string {
	var configured string
	if policyRoot != "" {
		cfg, _ := LoadGatewayConfig(policyRoot)
		configured = cfg.ScanTmpDir
	}
	return ResolveScanTmpDir(configured, filepath.Dir(bareDir))
}

// ResolveScanTmpDir picks the directory materialize worktrees are created in and
// makes sure it exists: the configured value wins, otherwise
// <reposRoot>/_scan-tmp. Returns "" when neither is usable, which leaves the
// caller on $TMPDIR - worse for RAM, but a gate that still runs beats a gate
// that rejects every push over a temp-dir preference.
func ResolveScanTmpDir(configured, reposRoot string) string {
	dir, _ := ResolveScanTmpDirChecked(configured, reposRoot)
	return dir
}

// ResolveScanTmpDirChecked is ResolveScanTmpDir plus the reason it gave up.
// The fallback to $TMPDIR is exactly the RAM-staging behaviour the setting
// exists to avoid, so the caller that hits it can say so out loud instead of
// degrading silently.
func ResolveScanTmpDirChecked(configured, reposRoot string) (string, error) {
	dir := configured
	if dir == "" {
		if reposRoot == "" {
			return "", fmt.Errorf("no repos root to derive a staging dir from")
		}
		// A hook derives its root from GIT_DIR, which resolves through the
		// <repos-root>/<name>.git symlink down into _repos/. Step back out, so
		// both layouts stage in one place - otherwise the maintenance sweeper
		// would hunt orphans in a directory the gate never writes to.
		if filepath.Base(reposRoot) == reposInternalsDir {
			reposRoot = filepath.Dir(reposRoot)
		}
		dir = filepath.Join(reposRoot, ScanTmpDirName)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("cannot use %s: %w", dir, err)
	}
	return dir, nil
}

// StagingInfo is where trees are actually staged, for display. Read-only:
// unlike ResolveScanTmpDirChecked it creates nothing, because rendering a
// dashboard page must not have side effects.
type StagingInfo struct {
	Dir        string // effective staging dir; "" means $TMPDIR
	Configured string // what [gateway] scan_tmpdir asked for; "" = the default
	Exists     bool   // false before the first push simply creates it
}

// InspectStagingDir computes the path ResolveScanTmpDirChecked would use.
func InspectStagingDir(configured, reposRoot string) StagingInfo {
	info := StagingInfo{Configured: configured}
	switch {
	case configured != "":
		info.Dir = configured
	case reposRoot == "":
		return info // $TMPDIR
	default:
		if filepath.Base(reposRoot) == reposInternalsDir {
			reposRoot = filepath.Dir(reposRoot)
		}
		info.Dir = filepath.Join(reposRoot, ScanTmpDirName)
	}
	if fi, err := os.Stat(info.Dir); err == nil && fi.IsDir() {
		info.Exists = true
	}
	return info
}
