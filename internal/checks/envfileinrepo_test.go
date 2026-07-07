// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"testing"

	"nimblegate/internal/engine"
)

func TestNoEnvFileInRepo_LiveEnvBlocks(t *testing.T) {
	root := t.TempDir()
	piiWrite(t, root, ".env", "DATABASE_URL=postgres://u:p@db/prod\nAPI_KEY=abc123\n")
	res := NoEnvFileInRepo(piiCtx(root))
	if res.Outcome != engine.OutcomeBlock {
		t.Fatalf("want BLOCK, got %v (%s)", res.Outcome, res.Reason)
	}
	if len(res.Hits) != 1 || res.Hits[0].Line != 1 {
		t.Fatalf("unexpected hits: %+v", res.Hits)
	}
}

func TestNoEnvFileInRepo_VariantBlocks(t *testing.T) {
	root := t.TempDir()
	piiWrite(t, root, "app/.env.production", "export SECRET_TOKEN=deadbeef\n")
	res := NoEnvFileInRepo(piiCtx(root))
	if res.Outcome != engine.OutcomeBlock {
		t.Fatalf("want BLOCK, got %v (%s)", res.Outcome, res.Reason)
	}
}

func TestNoEnvFileInRepo_ExampleAllowed(t *testing.T) {
	root := t.TempDir()
	piiWrite(t, root, ".env.example", "DATABASE_URL=postgres://user:pass@localhost/dev\n")
	piiWrite(t, root, ".env.sample", "API_KEY=your-key-here\n")
	piiWrite(t, root, ".env.local.template", "TOKEN=fill-me\n")
	res := NoEnvFileInRepo(piiCtx(root))
	if res.Outcome != engine.OutcomePass {
		t.Fatalf("want PASS, got %v (%s)", res.Outcome, res.Reason)
	}
}

func TestNoEnvFileInRepo_CommentsOnlyPasses(t *testing.T) {
	root := t.TempDir()
	piiWrite(t, root, ".env", "# see .env.example for the variables this app needs\n\n")
	res := NoEnvFileInRepo(piiCtx(root))
	if res.Outcome != engine.OutcomePass {
		t.Fatalf("want PASS, got %v (%s)", res.Outcome, res.Reason)
	}
}

func TestNoEnvFileInRepo_DisableMarkerHonored(t *testing.T) {
	root := t.TempDir()
	piiWrite(t, root, ".env", "# appframes:disable security/no-env-file-in-repo\nLOCAL_ONLY=1\n")
	res := NoEnvFileInRepo(piiCtx(root))
	if res.Outcome != engine.OutcomePass {
		t.Fatalf("want PASS, got %v (%s)", res.Outcome, res.Reason)
	}
}

func TestNoEnvFileInRepo_NonEnvFilePasses(t *testing.T) {
	root := t.TempDir()
	piiWrite(t, root, "config/environment.rb", "API_KEY=not-an-env-file\n")
	res := NoEnvFileInRepo(piiCtx(root))
	if res.Outcome != engine.OutcomePass {
		t.Fatalf("want PASS, got %v (%s)", res.Outcome, res.Reason)
	}
}
