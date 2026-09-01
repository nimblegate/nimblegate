// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"bytes"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
)

// This file declares the patterns the frame detects, so the frame must
// not report on it. Until the matcher required a standalone line, the
// marker const below suppressed this file by accident rather than by
// decision; the decision is now on the record.
// appframes:disable security/no-kubeconfig-in-repo

const kubeconfigDisableMarker = "appframes:disable security/no-kubeconfig-in-repo"
const kubeconfigDisableLineMarker = "appframes:disable-next-line security/no-kubeconfig-in-repo"

// kubeconfigBasenames are the conventional kubeconfig filenames. A file by
// this name additionally needs the document shape (clusters: + users:) to
// fire, so an unrelated file that happens to share the name stays quiet.
var kubeconfigBasenames = map[string]bool{
	"kubeconfig":      true,
	".kubeconfig":     true,
	"kubeconfig.yaml": true,
	"kubeconfig.yml":  true,
}

// NoKubeconfigInRepo detects Kubernetes client credentials committed to the
// repository. `client-key-data:` is base64-encoded private key material -
// cluster admin access in one line - and fires wherever it appears. The
// conventional kubeconfig filenames fire when the file also has kubeconfig
// document shape.
//
// Scope contract follows the standard file-scan scope conventions:
//   - cli + empty ChangedFiles → project-wide walk
//   - pre-commit + ChangedFiles → those only
//   - pre-commit + empty → PASS
//   - noise-dir exclusion uniform
//
// Redaction guarantee: key material is NEVER echoed. Reason reports
// file:line:label only.
func NoKubeconfigInRepo(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "security/no-kubeconfig-in-repo",
		Category: frames.CategorySecurity,
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
			files = append(files, path)
			return nil
		})
	}

	var hits []string
	var hitsStruct []engine.Hit
	const hitCap = 10

filesLoop:
	for _, file := range files {
		if ShouldSkipPath(ctx, file) {
			continue
		}
		data, ok := ReadFileBounded(file, DefaultMaxFileBytes)
		if !ok {
			continue
		}
		if bytes.IndexByte(data, 0) >= 0 {
			continue
		}
		content := string(data)
		if fileDisabledByMarker(content, kubeconfigDisableMarker) {
			continue
		}

		// --- content detection: client key material fires anywhere ---
		lines := strings.Split(content, "\n")
		contentMatched := false
		for i, line := range lines {
			if i > 0 && lineCarriesMarker(lines[i-1], kubeconfigDisableLineMarker) {
				continue
			}
			if strings.Contains(line, "client-key-data:") {
				contentMatched = true
				label := "kubeconfig client key material (client-key-data)"
				hits = append(hits, fmt.Sprintf("%s:%d - %s", file, i+1, label))
				hitsStruct = append(hitsStruct, engine.Hit{File: file, Line: i + 1, Label: label})
				if len(hits) >= hitCap {
					break filesLoop
				}
				break // one hit per file is enough
			}
		}
		if contentMatched {
			continue
		}

		// --- filename detection: conventional name + document shape ---
		if kubeconfigBasenames[filepath.Base(file)] &&
			strings.Contains(content, "clusters:") && strings.Contains(content, "users:") {
			label := fmt.Sprintf("kubeconfig file %q", filepath.Base(file))
			hits = append(hits, fmt.Sprintf("%s:0 - %s", file, label))
			hitsStruct = append(hitsStruct, engine.Hit{File: file, Line: 0, Label: label})
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
	res.Reason = "Kubernetes client credentials committed (content redacted): " + strings.Join(hits, "; ")
	res.Fix = "remove the kubeconfig from the push and revoke the client certificate / rotate the cluster credentials (assume compromised); distribute cluster access via your identity provider or short-lived tokens, never via committed kubeconfig files"
	return res
}
