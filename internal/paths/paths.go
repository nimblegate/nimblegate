// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

// Package paths provides helpers for finding the project root and the
// project's .appframes/ directory.
package paths

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	projectConfigFile = "appframes.toml"
	projectDir        = ".appframes"
)

// FindProjectRoot walks up from start until it finds a directory containing
// appframes.toml. Returns the absolute path to that directory. Errors if
// none is found before reaching the filesystem root.
func FindProjectRoot(start string) (string, error) {
	abs, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	dir := abs
	for {
		if _, err := os.Stat(filepath.Join(dir, projectConfigFile)); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("nimblegate: no %s found in %s or any ancestor", projectConfigFile, abs)
		}
		dir = parent
	}
}

// AppframesDir returns the path to the project's .appframes/ subdirectory.
// Does NOT verify the dir exists - frames-from-stdlib-only is a valid setup.
func AppframesDir(projectRoot string) string {
	return filepath.Join(projectRoot, projectDir)
}

// ConfigPath returns the path to the project's appframes.toml.
func ConfigPath(projectRoot string) string {
	return filepath.Join(projectRoot, projectConfigFile)
}

// AuditLogPath returns the path to the project's audit log file.
func AuditLogPath(projectRoot string) string {
	return filepath.Join(projectRoot, projectDir, "audit.log")
}
