// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package v1tov2_test

import (
	"strings"
	"testing"

	v2 "nimblegate/internal/config/v2"
	"nimblegate/internal/migration/v1tov2"
)

func TestValidateInternalConsistency_myappReal(t *testing.T) {
	cfg := v1tov2.Translate(v1tov2.Input{
		AppliedKits: []string{"cf-pages-project", "security-strict"},
	})
	if err := v1tov2.ValidateInternalConsistency(cfg); err != nil {
		t.Errorf("myapp-real translation should pass internal consistency, got: %v", err)
	}
}

func TestValidateInternalConsistency_rejectsNil(t *testing.T) {
	if err := v1tov2.ValidateInternalConsistency(nil); err == nil {
		t.Error("expected error for nil cfg")
	}
}

func TestValidateInternalConsistency_rejectsMismatchedExclude(t *testing.T) {
	// Construct an inconsistent config manually: platform selected as cloudflare
	// but per-vendor exclude list under aws. The translator never produces this
	// directly, but a malformed config should be rejected.
	cfg := &v2.Config{
		Platform:          v2.PlatformSel{Selected: "cloudflare"},
		PlatformOverrides: map[string]v2.VendorOverride{"aws": {Exclude: []string{"aws-rds"}}},
	}
	cfg.Appframes.Schema.Version = 2
	err := v1tov2.ValidateInternalConsistency(cfg)
	if err == nil {
		t.Error("expected error when exclude list vendor differs from selected platform")
	}
	if !strings.Contains(err.Error(), "aws") {
		t.Errorf("error should mention the mismatched vendor, got: %v", err)
	}
}

// (Earlier stub test removed - ValidateZeroDelta now wired up as
// ValidateZeroLoss with engine integration; covered by the engine-level
// TestV1V2NoLoss tests + migrate-config CLI tests.)
