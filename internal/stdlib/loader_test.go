// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package stdlib

import (
	"testing"
)

func TestLoadStdlibFrames_LoadsAtLeastOne(t *testing.T) {
	frames, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if len(frames) == 0 {
		t.Fatal("Load() returned 0 frames; expected at least 1 (folder-branch-lock)")
	}
	found := false
	for _, f := range frames {
		if f.ID() == "git/folder-branch-lock" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Load() did not return folder-branch-lock")
	}
}

func TestLoadStdlibFrames_LoadsAllExpectedFrames(t *testing.T) {
	frames, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	// V0 (5) + V0.5 Tier 6 doc-enforcement (3) + V0.5 Tier 1
	// catastrophic-prevention (no-hardcoded-credentials, no-private-keys-in-repo,
	// rm-rf-protected-paths, curl-pipe-shell, no-amend-pushed-commits).
	wantIDs := map[string]bool{
		"git/folder-branch-lock":                       true,
		"git/no-amend-pushed-commits":                  true,
		"git/no-bypass-pre-commit":                     true,
		"git/no-force-push-main":                       true,
		"git/no-lfsconfig-changes":                     true,
		"commands/apt-purge-preview":                   true,
		"commands/curl-pipe-shell":                     true,
		"database/migration-script-explicit-env":       true,
		"database/migration-verification-step":         true,
		"database/sqlite-migration-idempotent-wrapper": true,
		"filesystem/rm-rf-protected-paths":             true,
		"network/cidr-host-bits-zero":                  true,
		"network/no-localhost-in-proxy-config":         true,
		"security/no-innerHTML-user-input":             true,
		"security/no-hardcoded-credentials":            true,
		"security/no-private-keys-in-repo":             true,
		"app-correctness/cf-graphql-dataset-by-window": true,
		"app-correctness/cf-graphql-schema-match":      true,
		"app-correctness/dynamic-env-declared":         true,
		"app-correctness/prefer-static-public":         true,
		"database/schema-vs-code-drift":                true,
		"app-correctness/top-of-page-import-safety":    true,
		"security/no-mixed-content-urls":               true,
		"security/cf-pages-headers-baseline":           true,
		"security/no-bidi-override":                    true,
		"security/no-invisible-tag-chars":              true,
		"security/no-zero-width-in-source":             true,
		"security/no-homoglyph-identifiers":            true,
		"encoding/no-bom":                              true,
		"encoding/no-smart-quotes-in-config":           true,
		"encoding/yaml-no-tabs":                        true,
		"encoding/consistent-line-endings":             true,
		"encoding/no-mixed-indent":                     true,
		"encoding/no-en-dash-in-commands":              true,
		"encoding/no-non-printable":                    true,
		"encoding/no-zero-width-in-content":            true,
		"documentation/cross-branch-id-consistency":    true,
		"documentation/dated-todo":                     true,
		"documentation/doc-touches-with-code":          true,
		"documentation/markdown-link-check-internal":   true,
		"web/html-required-meta":                       true,
		"web/html-seo-meta":                            true,
		"web/html-img-alt":                             true,
		"web/html-markup-valid":                        true,
		"web/html-placeholder-content":                 true,
	}
	for _, f := range frames {
		delete(wantIDs, f.ID())
	}
	if len(wantIDs) != 0 {
		t.Errorf("missing frames: %v", wantIDs)
	}
}

func TestLoadStdlibFrames_CategoryIsCanonical(t *testing.T) {
	canonical := map[string]struct{}{
		"security": {}, "network": {}, "filesystem": {},
		"git": {}, "commands": {}, "app-correctness": {},
		"database": {}, "web": {}, "documentation": {},
		"platform": {}, "framework": {},
		"encoding": {},
	}
	frames, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range frames {
		if _, ok := canonical[string(f.Frontmatter.Category)]; !ok {
			t.Errorf("frame %s: category=%q is not one of the 12 canonical values",
				f.ID(), f.Frontmatter.Category)
		}
	}
}

func TestLoadStdlibFrames_SubcategoryPresent(t *testing.T) {
	frames, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range frames {
		if f.Frontmatter.Subcategory == "" {
			t.Errorf("frame %s: subcategory is empty - every stdlib frame must declare one",
				f.ID())
		}
	}
}

// TestLoadStdlibFrames_TierMetadataPresent verifies every shipped stdlib
// frame carries an explicit `tier:` (1-6). Catastrophic-prevention frames
// must be tier 1; doc-enforcement frames must be tier 6. The retrofit was
// done in slice 1 of frame-management; if a new stdlib frame is added
// without tier, this test catches it.
func TestLoadStdlibFrames_TierMetadataPresent(t *testing.T) {
	frames, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	wantTier := map[string]int{
		"git/folder-branch-lock":                       1,
		"git/no-amend-pushed-commits":                  1,
		"git/no-bypass-pre-commit":                     1,
		"git/no-force-push-main":                       1,
		"git/no-lfsconfig-changes":                     1,
		"commands/apt-purge-preview":                   1,
		"commands/curl-pipe-shell":                     1,
		"database/migration-script-explicit-env":       1,
		"database/migration-verification-step":         1,
		"database/sqlite-migration-idempotent-wrapper": 1,
		"filesystem/rm-rf-protected-paths":             1,
		// Tier reclassifications recorded during the 2026-06-03 Phase 2
		// content audit. Justification per frame body:
		//   cidr-host-bits-zero 2→3: silent config rejection / wrong routing
		//     is correctness-class, not the exploit surface Tier 2 names.
		//   no-localhost-in-proxy-config 2→1: body says "every request fails
		//     with connection refused" - total service outage = Tier 1.
		//   dynamic-env-declared 2→1: body says "App dead in prod until
		//     rollback" - catastrophic-class even though category is correctness.
		//   dated-todo 3→6: TODO-formatting is doc/convention, not runtime correctness.
		//   html-markup-valid 3→6: HTML validity is convention, not runtime.
		//   html-placeholder-content 3→6: placeholder text shipping = convention/embarrassment.
		"network/cidr-host-bits-zero":                  3,
		"network/no-localhost-in-proxy-config":         1,
		"security/no-hardcoded-credentials":            1,
		"security/no-private-keys-in-repo":             1,
		"security/no-innerHTML-user-input":             2,
		"app-correctness/cf-graphql-dataset-by-window": 3,
		"app-correctness/cf-graphql-schema-match":      3,
		"app-correctness/dynamic-env-declared":         1,
		"app-correctness/prefer-static-public":         3,
		"database/schema-vs-code-drift":                1,
		"app-correctness/top-of-page-import-safety":    3,
		"security/no-mixed-content-urls":               2,
		"security/cf-pages-headers-baseline":           2,
		"security/no-bidi-override":                    1,
		"security/no-invisible-tag-chars":              1,
		"security/no-zero-width-in-source":             1,
		"security/no-homoglyph-identifiers":            2,
		"encoding/no-bom":                              2,
		"encoding/no-smart-quotes-in-config":           1,
		"encoding/yaml-no-tabs":                        1,
		"encoding/consistent-line-endings":             2,
		"encoding/no-mixed-indent":                     2,
		"encoding/no-en-dash-in-commands":              1,
		"encoding/no-non-printable":                    3,
		"encoding/no-zero-width-in-content":            3,
		"documentation/dated-todo":                     6,
		"documentation/cross-branch-id-consistency":    6,
		"documentation/doc-touches-with-code":          6,
		"documentation/markdown-link-check-internal":   6,
		"web/html-required-meta":                       6,
		"web/html-seo-meta":                            6,
		"web/html-img-alt":                             6,
		"web/html-markup-valid":                        6,
		"web/html-placeholder-content":                 6,
	}
	for _, f := range frames {
		want, ok := wantTier[f.ID()]
		if !ok {
			t.Errorf("frame %s has no expected tier (add it to wantTier)", f.ID())
			continue
		}
		if f.Frontmatter.Tier == 0 {
			t.Errorf("frame %s: tier missing - every stdlib frame must set tier explicitly", f.ID())
			continue
		}
		if f.Frontmatter.Tier != want {
			t.Errorf("frame %s: tier = %d, want %d", f.ID(), f.Frontmatter.Tier, want)
		}
	}
}
