// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package maintenance

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoad_missingFileReturnsDefaults(t *testing.T) {
	tmp := t.TempDir()
	cfg, err := Load(filepath.Join(tmp, "gateway.toml"))
	if err != nil {
		t.Fatalf("expected no error on missing file; got %v", err)
	}
	if !cfg.Enabled {
		t.Errorf("default Enabled = false; want true")
	}
	if cfg.Interval != 168*time.Hour {
		t.Errorf("default Interval = %s; want 168h", cfg.Interval)
	}
}

func TestLoad_emptySectionReturnsDefaults(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "gateway.toml")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("empty file: %v", err)
	}
	if !cfg.Enabled || cfg.Interval != 168*time.Hour {
		t.Errorf("empty file should produce defaults; got %+v", cfg)
	}
}

func TestLoad_partialSectionFillsDefaults(t *testing.T) {
	// Only interval set - Enabled should fall back to default true.
	tmp := t.TempDir()
	path := filepath.Join(tmp, "gateway.toml")
	if err := os.WriteFile(path, []byte("[maintenance]\ninterval = \"24h\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Enabled {
		t.Errorf("Enabled should fall back to default true; got false")
	}
	if cfg.Interval != 24*time.Hour {
		t.Errorf("Interval = %s; want 24h", cfg.Interval)
	}
}

func TestLoad_explicitDisabledOverridesDefault(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "gateway.toml")
	if err := os.WriteFile(path, []byte("[maintenance]\nenabled = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Enabled {
		t.Errorf("Enabled should be false (explicit); got true")
	}
}

func TestLoad_invalidTOMLReturnsError(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "gateway.toml")
	if err := os.WriteFile(path, []byte("not = valid = toml"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Error("expected parse error on invalid TOML")
	}
}

func TestLoad_invalidDurationReturnsError(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "gateway.toml")
	if err := os.WriteFile(path, []byte("[maintenance]\ninterval = \"not-a-duration\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Error("expected duration parse error")
	}
}

func TestLoad_intervalBelowMinimumRejected(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "gateway.toml")
	if err := os.WriteFile(path, []byte("[maintenance]\ninterval = \"5s\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Error("expected rejection of sub-minute interval")
	}
}

func TestParse_AuditAndEventsRetention(t *testing.T) {
	in := `
[maintenance]
[maintenance.audit]
accept_retention = "240h"
reject_retention = "8760h"
[maintenance.events]
retention = "120h"
`
	cfg, err := parse([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuditAcceptRetention != 240*time.Hour {
		t.Fatalf("accept: got %s", cfg.AuditAcceptRetention)
	}
	if cfg.AuditRejectRetention != 8760*time.Hour {
		t.Fatalf("reject: got %s", cfg.AuditRejectRetention)
	}
	if cfg.EventsRetention != 120*time.Hour {
		t.Fatalf("events: got %s", cfg.EventsRetention)
	}
}

func TestParse_RetentionDefaultsWhenAbsent(t *testing.T) {
	cfg, err := parse([]byte("[maintenance]\n"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AuditAcceptRetention != 30*24*time.Hour || cfg.AuditRejectRetention != 0 || cfg.EventsRetention != 30*24*time.Hour {
		t.Fatalf("defaults wrong: %+v", cfg)
	}
}

func TestParse_NegativeAcceptRetentionErrors(t *testing.T) {
	_, err := parse([]byte("[maintenance]\n[maintenance.audit]\naccept_retention = \"-1h\"\n"))
	if err == nil {
		t.Fatal("want error for negative accept_retention")
	}
}

func TestLoad_unrelatedSectionsIgnored(t *testing.T) {
	// Future [other-section] keys shouldn't trip the parser.
	tmp := t.TempDir()
	path := filepath.Join(tmp, "gateway.toml")
	body := `
[maintenance]
interval = "12h"

[future-feature]
some-key = "some-value"
nested = { a = 1, b = 2 }
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("unrelated section broke parse: %v", err)
	}
	if cfg.Interval != 12*time.Hour {
		t.Errorf("Interval = %s; want 12h", cfg.Interval)
	}
}
