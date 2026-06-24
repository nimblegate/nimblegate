// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

// Package stdlib embeds the built-in frame catalog and provides a loader.
package stdlib

import "embed"

//go:embed all:frames
var frameFS embed.FS

// testdataFS holds the per-frame negative-selection corpora
// (testdata/<category>/<name>/{positives,negatives}/*). Embedded
// separately from frames/ so the selection runner can read it without
// the frame loader having to skip yet another subdir. Added 2026-05-20
// with Phase 1 Slice 2.
//
//go:embed all:testdata
var testdataFS embed.FS

// V2FS embeds the v2 bucket layout at internal/stdlib/v2/. Each frame file's
// path under v2/ encodes its v2 bucket (e.g., domains/security/no-hardcoded-
// credentials.md), while the file's frontmatter retains the v1 name+category
// fields used for the frame's stable ID (Frame.ID() = category/name).
//
// The map of v1-frame-ID → v2-bucket-path is built by walking V2FS at
// runtime; see internal/engine/v2resolve.go.
//
//go:embed all:v2
var V2FS embed.FS
