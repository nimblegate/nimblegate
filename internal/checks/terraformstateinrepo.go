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

const tfstateDisableMarker = "appframes:disable security/no-terraform-state-in-repo"

// tfstateContentMatch reports whether the content looks like a Terraform
// state document: every real state file carries both keys regardless of
// resource content or Terraform version.
func tfstateContentMatch(content string) bool {
	return strings.Contains(content, `"terraform_version"`) &&
		strings.Contains(content, `"lineage"`)
}

// tfstateBasenameMatch catches the conventional filenames, including the
// automatic backup Terraform writes next to the state.
func tfstateBasenameMatch(base string) bool {
	return strings.HasSuffix(base, ".tfstate") || strings.HasSuffix(base, ".tfstate.backup")
}

// NoTerraformStateInRepo detects Terraform state committed to the
// repository. State files hold every provider credential, connection
// string, and resource attribute in PLAINTEXT, including values marked
// sensitive in configuration - state belongs in a remote backend, never in
// git. Two detection paths: content markers (catches state renamed to
// dodge filename rules) and the conventional `*.tfstate` /
// `*.tfstate.backup` filenames.
//
// Scope contract follows the standard file-scan scope conventions:
//   - cli + empty ChangedFiles → project-wide walk
//   - pre-commit + ChangedFiles → those only
//   - pre-commit + empty → PASS
//   - noise-dir exclusion uniform
//
// Redaction guarantee: state content is NEVER echoed.
func NoTerraformStateInRepo(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "security/no-terraform-state-in-repo",
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

	for _, file := range files {
		if ShouldSkipPath(ctx, file) {
			continue
		}
		base := filepath.Base(file)
		nameMatch := tfstateBasenameMatch(base)

		data, ok := ReadFileBounded(file, DefaultMaxFileBytes)
		contentMatch := false
		if ok && bytes.IndexByte(data, 0) < 0 {
			content := string(data)
			if strings.Contains(content, tfstateDisableMarker) {
				continue
			}
			contentMatch = tfstateContentMatch(content)
		}

		var label string
		switch {
		case contentMatch:
			label = "Terraform state content (terraform_version + lineage)"
		case nameMatch:
			label = fmt.Sprintf("Terraform state filename %q", base)
		default:
			continue
		}
		hits = append(hits, fmt.Sprintf("%s:0 - %s", file, label))
		hitsStruct = append(hitsStruct, engine.Hit{File: file, Line: 0, Label: label})
		if len(hits) >= hitCap {
			break
		}
	}

	if len(hits) == 0 {
		res.Outcome = engine.OutcomePass
		return res
	}
	res.Hits = hitsStruct
	res.Outcome = engine.OutcomeBlock
	res.Reason = "Terraform state committed (content redacted): " + strings.Join(hits, "; ")
	res.Fix = "remove the state from the push and rotate every credential it contains (state stores sensitive values in plaintext); move state to a remote backend (S3 + locking, Terraform Cloud, or your host's equivalent) and add `*.tfstate*` to .gitignore"
	return res
}
