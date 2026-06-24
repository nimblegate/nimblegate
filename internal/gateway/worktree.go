// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"nimblegate/internal/scanignore"
)

// materializeTree exports the tree at rev from the bare repo gitDir into destDir
// using `git archive | tar -x`. destDir must already exist.
func materializeTree(gitDir, rev, destDir string) error {
	archive := exec.Command("git", "--git-dir", gitDir, "archive", "--format=tar", rev)
	var aerr bytes.Buffer
	archive.Stderr = &aerr
	untar := exec.Command("tar", "-x", "-C", destDir)
	pipe, err := archive.StdoutPipe()
	if err != nil {
		return err
	}
	untar.Stdin = pipe
	if err := untar.Start(); err != nil {
		return err
	}
	if err := archive.Run(); err != nil {
		_ = untar.Wait() // reap the child even on failure
		return fmt.Errorf("git archive %s: %w: %s", rev, err, strings.TrimSpace(aerr.String()))
	}
	if err := untar.Wait(); err != nil {
		return fmt.Errorf("untar: %w", err)
	}
	return nil
}

// overlayPolicy copies the gateway-held appframes.toml (and a .appframes/ dir if
// present) from policyDir onto destDir, overwriting any pushed config. This is
// what makes the enforced policy gateway-held: the pushed tree's own config is
// replaced before the check runs.
func overlayPolicy(policyDir, destDir string) error {
	// Remove config the push brought in, so the enforced policy is purely
	// gateway-held - a push cannot shadow/downgrade frames via its own config.
	_ = os.Remove(filepath.Join(destDir, "appframes.toml"))
	_ = os.RemoveAll(filepath.Join(destDir, ".appframes"))

	// Strip any pushed .appframes-ignore markers anywhere in the tree - they are
	// a scan-policy surface the push must NOT control (same reason we wipe
	// appframes.toml). The engine discovers these markers tree-wide; leaving one
	// in place would let a push exclude files (e.g. "*.pem") from the gate.
	_ = filepath.WalkDir(destDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // best-effort cleanup
		}
		if !d.IsDir() && d.Name() == scanignore.MarkerFilename {
			_ = os.Remove(p)
		}
		return nil
	})

	src := filepath.Join(policyDir, "appframes.toml")
	if b, err := os.ReadFile(src); err == nil {
		if err := os.WriteFile(filepath.Join(destDir, "appframes.toml"), b, 0o644); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if fi, err := os.Stat(filepath.Join(policyDir, ".appframes")); err == nil && fi.IsDir() {
		return copyDir(filepath.Join(policyDir, ".appframes"), filepath.Join(destDir, ".appframes"))
	}
	return nil
}

// copyDir recursively copies src to dst (files 0644, dirs 0755).
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	})
}
