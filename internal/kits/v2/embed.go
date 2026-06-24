// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

// Package v2 is the v2 kit catalog: named pre-filled axis-selection bundles
// operators can apply via `nimblegate kits apply <kit_id>` to bootstrap an
// appframes.toml v2 config. v2 kits hold AXIS SELECTIONS (framework + platform
// + platform_exclude + domains), not explicit frame lists - frames flow
// from the selections via the buckets.Selection resolution chain.
//
// Distinct from internal/kits/ (v1) which holds frame-list-style kits used by
// the legacy v1 engine. The two packages coexist during the migration window;
// v1 kits stay until the v2 dashboard UI ships and operators have a path to
// migrate.
//
// Spec: docs/superpowers/specs/2026-06-05-kit-architecture-three-axis-design.md §7
package v2

import "embed"

//go:embed stdlib.toml
var stdlibFS embed.FS
