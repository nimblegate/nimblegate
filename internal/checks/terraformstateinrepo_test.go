// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"strings"
	"testing"

	"nimblegate/internal/engine"
)

// This file is the frame's own fixture corpus, so the frame must not
// report on it. Until the matcher required a standalone line, a marker
// quoted inside a fixture suppressed this file by accident rather than
// by decision; the decision is now on the record.
// appframes:disable security/no-terraform-state-in-repo

const tfstateSample = `{
  "version": 4,
  "terraform_version": "1.9.0",
  "serial": 12,
  "lineage": "3f0c6a7e-0000-0000-0000-000000000000",
  "resources": []
}
`

func TestNoTerraformStateInRepo_FilenameBlocks(t *testing.T) {
	root := t.TempDir()
	piiWrite(t, root, "infra/terraform.tfstate", tfstateSample)
	res := NoTerraformStateInRepo(piiCtx(root))
	if res.Outcome != engine.OutcomeBlock {
		t.Fatalf("want BLOCK, got %v (%s)", res.Outcome, res.Reason)
	}
	if !strings.Contains(res.Reason, "terraform_version + lineage") {
		t.Fatalf("content detection should win over filename: %s", res.Reason)
	}
}

func TestNoTerraformStateInRepo_BackupFilenameBlocks(t *testing.T) {
	root := t.TempDir()
	piiWrite(t, root, "terraform.tfstate.backup", "{}\n")
	res := NoTerraformStateInRepo(piiCtx(root))
	if res.Outcome != engine.OutcomeBlock {
		t.Fatalf("want BLOCK, got %v (%s)", res.Outcome, res.Reason)
	}
}

func TestNoTerraformStateInRepo_RenamedStateBlocksByContent(t *testing.T) {
	root := t.TempDir()
	piiWrite(t, root, "backup/state.json", tfstateSample)
	res := NoTerraformStateInRepo(piiCtx(root))
	if res.Outcome != engine.OutcomeBlock {
		t.Fatalf("want BLOCK, got %v (%s)", res.Outcome, res.Reason)
	}
}

func TestNoTerraformStateInRepo_TfConfigPasses(t *testing.T) {
	root := t.TempDir()
	piiWrite(t, root, "main.tf", "resource \"aws_s3_bucket\" \"b\" {\n  bucket = \"my-bucket\"\n}\n")
	piiWrite(t, root, "notes.md", "run terraform apply and check the lineage of changes\n")
	res := NoTerraformStateInRepo(piiCtx(root))
	if res.Outcome != engine.OutcomePass {
		t.Fatalf("want PASS, got %v (%s)", res.Outcome, res.Reason)
	}
}

func TestNoTerraformStateInRepo_DisableMarkerHonored(t *testing.T) {
	root := t.TempDir()
	piiWrite(t, root, "fixtures/state.json",
		"// appframes:disable security/no-terraform-state-in-repo\n"+tfstateSample)
	res := NoTerraformStateInRepo(piiCtx(root))
	if res.Outcome != engine.OutcomePass {
		t.Fatalf("want PASS, got %v (%s)", res.Outcome, res.Reason)
	}
}
