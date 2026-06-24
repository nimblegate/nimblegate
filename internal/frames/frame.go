// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

// Package frames defines the in-memory representation of a frame and
// utilities for parsing frame markdown files (YAML frontmatter + body).
package frames

// Severity is how strongly a frame's failure is enforced.
type Severity string

const (
	SeverityBlock Severity = "BLOCK"
	SeverityWarn  Severity = "WARN"
	SeverityInfo  Severity = "INFO"
)

// Category groups frames for organization + output ordering.
// The order of the constants matches priority (lower index = more destructive).
type Category string

const (
	CategoryGitSafety      Category = "git"
	CategoryFilesystem     Category = "filesystem"
	CategoryCommands       Category = "commands"
	CategoryNetwork        Category = "network"
	CategorySecurity       Category = "security"
	CategoryAppCorrectness Category = "app-correctness"
	CategoryDatabase       Category = "database"
	CategoryWeb            Category = "web"
	CategoryDocumentation  Category = "documentation"
	CategoryPlatform       Category = "platform"
	CategoryFramework      Category = "framework"
	CategoryEncoding       Category = "encoding"
)

// CategoryPriority returns a numeric priority for a category (1 = most destructive).
// Used for output ordering when multiple frames fail. Not used for execution gating.
func CategoryPriority(c Category) int {
	switch c {
	case CategoryGitSafety:
		return 1
	case CategoryFilesystem:
		return 2
	case CategoryCommands:
		return 3
	case CategoryNetwork:
		return 4
	case CategorySecurity:
		return 5
	case CategoryAppCorrectness:
		return 6
	case CategoryDatabase:
		return 7
	case CategoryWeb:
		return 8
	case CategoryDocumentation:
		return 9
	case CategoryPlatform:
		return 10
	case CategoryFramework:
		return 11
	case CategoryEncoding:
		return 12
	}
	return 99
}

// AppliesTo declares what kinds of targets a frame examines.
type AppliesTo struct {
	Files    []string `yaml:"files"`
	Commands []string `yaml:"commands"`
}

// Lifecycle is the deployment state of a frame in the
// proposed → candidate → active → deprecated → archived flow introduced
// 2026-05-20 by the Phase 1 pattern-frame architecture.
//
// The intent: frames must EARN deployment (via negative selection +
// observed hits) and EARN their continued place (via apoptosis when
// stale). Lifecycle makes both visible.
type Lifecycle string

const (
	LifecycleProposed   Lifecycle = "proposed"
	LifecycleCandidate  Lifecycle = "candidate"
	LifecycleActive     Lifecycle = "active"
	LifecycleDeprecated Lifecycle = "deprecated"
	LifecycleArchived   Lifecycle = "archived"
)

// Frontmatter is the machine-readable YAML metadata at the top of every frame markdown file.
//
// Tier, Tags, DedupKey, and RunsAfter were added in V0.5 (frame management +
// chaining). All four are optional with zero-value defaults so existing
// project frames continue to load without modification.
type Frontmatter struct {
	Name     string   `yaml:"name"`
	Category Category `yaml:"category"`
	Severity Severity `yaml:"severity"`
	Triggers []string `yaml:"triggers"`

	// SeveritySource selects who decides a fired frame's gate outcome.
	// Empty/"frontmatter" (default): the runner makes Severity (after any
	// config override) authoritative - the CheckFunc only signals fired/not.
	// "frame": the CheckFunc's own outcome stands, for frames that emit
	// different severities by confidence (e.g. credentials/keys: BLOCK on a
	// confirmed hit, INFO on a weak one).
	SeveritySource string `yaml:"severity-source"`

	AppliesTo     AppliesTo `yaml:"applies-to"`
	CanonicalRefs []string  `yaml:"canonical-refs"`

	// Tier (1-6) drives display ordering and group membership.
	// 1 = catastrophic prevention; 6 = cosmetic / doc enforcement.
	// 0 (the zero value) means absent; consumers should call EffectiveTier.
	Tier int `yaml:"tier"`

	// Tags is a free-form list for cross-cutting filters
	// (e.g. ["secrets", "supply-chain"]). No fixed vocabulary.
	Tags []string `yaml:"tags"`

	// Optional; empty means the frame is direct-listed under its Category with no subcategory grouping.
	Subcategory string `yaml:"subcategory" toml:"subcategory" json:"subcategory"`

	// Optional; populated values cross-list the frame under Platform > <value> in addition to its primary Category placement.
	Platform []string `yaml:"platform" toml:"platform" json:"platform"`

	// Optional; populated values cross-list the frame under Framework > <value> in addition to its primary Category placement.
	Framework []string `yaml:"framework" toml:"framework" json:"framework"`

	// DedupKey controls participation in the dedup pass. Empty = no dedup.
	// Valid values: "file" or "file:line". Frames sharing the same DedupKey
	// AND scope collapse to one rendered row listing all firing frame IDs.
	DedupKey string `yaml:"dedup-key"`

	// RunsAfter lists frame IDs this frame should display after (best-effort
	// ordering for predictable output / tests). Not a dependency graph -
	// frames still run independently.
	RunsAfter []string `yaml:"runs-after"`

	// TimeCostHoursPrevented is the per-hit time savings estimate used by
	// `nimblegate audit analyze`. Optional. When omitted, the analyzer falls
	// back to the project-level [time-estimates] override for the frame's
	// tier, and then to the built-in tier default. Zero (the absent value)
	// means "use the tier default" - to declare zero explicitly, set a
	// negligibly small positive number like 0.01.
	TimeCostHoursPrevented float64 `yaml:"time-cost-hours-prevented"`

	// Pattern is the ID of the parent pattern this frame is an instance
	// of (e.g. "shared-history-rewrite"). Empty means unattributed.
	// Added 2026-05-20 with the Phase 1 architecture; existing frames
	// were back-filled. New frames should declare their parent pattern.
	Pattern string `yaml:"pattern"`

	// Lifecycle is the frame's deployment state. Empty defaults to
	// LifecycleActive via EffectiveLifecycle() for backward compat
	// with pre-Phase-1 frames. Added 2026-05-20.
	Lifecycle Lifecycle `yaml:"lifecycle"`

	// SelectionGrade summarizes the frame's negative-selection test
	// status. Common values: "pre-architecture" (predates the gating
	// system), "pending" (no testdata yet), "passing", "failing".
	// Slice 2 added structured SelectionStats below; this field
	// remains the quick summary. Added 2026-05-20.
	SelectionGrade string `yaml:"selection-grade"`

	// SelectionStats carries the per-corpus breakdown computed by
	// `nimblegate frame test --write-grade`. Optional - frames without
	// testdata or with grade "pre-architecture" leave it empty.
	// Added 2026-05-20 with Slice 2.
	SelectionStats SelectionStats `yaml:"selection-stats,omitempty"`

	// ArchivedAt is when the frame transitioned to lifecycle archived
	// (ISO-8601 UTC). Set by `nimblegate frame archive`; cleared by
	// `nimblegate frame revive`. Added 2026-05-20 with Slice 3.
	ArchivedAt string `yaml:"archived-at,omitempty"`

	// ArchiveReason documents why the frame was archived (optional,
	// free text - references an incident, platform change, supersession).
	// Added 2026-05-20 with Slice 3.
	ArchiveReason string `yaml:"archive-reason,omitempty"`
}

// SelectionStats is the structured breakdown of a selection run. The
// "N/M" strings are kept as strings (not pairs of ints) so the YAML
// reads naturally and humans can edit them without breaking the parse.
type SelectionStats struct {
	Positives string `yaml:"positives,omitempty"` // e.g. "5/5"
	Negatives string `yaml:"negatives,omitempty"` // e.g. "8/8"
	LastRun   string `yaml:"last-run,omitempty"`  // ISO-8601 UTC
}

// EffectiveLifecycle returns the lifecycle state used for filtering and
// display, substituting LifecycleActive when the frame omits the field.
// Pre-Phase-1 frames without explicit lifecycle continue to behave as
// active by default.
func (fm Frontmatter) EffectiveLifecycle() Lifecycle {
	if fm.Lifecycle == "" {
		return LifecycleActive
	}
	return fm.Lifecycle
}

// IsGated reports whether a frame in the given lifecycle state should
// participate in active gating (counted by `nimblegate check`, fired at
// pre-commit / git-wrap surfaces). Only `active` and `candidate` are
// gated; `proposed` is awaiting negative selection, `deprecated` and
// `archived` are historical record-only. Added 2026-05-20 with Slice 3.
func IsGated(l Lifecycle) bool {
	switch l {
	case LifecycleActive, LifecycleCandidate, "":
		return true
	}
	return false
}

// DefaultTimeCostHoursPreventedByTier is the conservative built-in mapping
// from tier to per-hit time estimate (hours). Used by `audit analyze` when
// neither the frame frontmatter nor the project config overrides it.
//
// Index 0 is unused; tiers are 1-6. Numbers are intentionally on the low end
// - better to undersell prevented-time than to claim implausibly large savings.
var DefaultTimeCostHoursPreventedByTier = [7]float64{
	0,    // unused (tiers are 1-6)
	4.0,  // tier 1 - catastrophic (credential leak, history rewrite, fs wipe)
	2.0,  // tier 2 - app-security (XSS, injection)
	0.5,  // tier 3 - code hygiene (TODOs, drift)
	0.25, // tier 4 - minor compliance
	0.1,  // tier 5
	0.1,  // tier 6 - doc enforcement
}

// EffectiveTier returns the tier value used for ordering and group resolution,
// substituting the default (3 = warn-grade) when the frame omits the field.
func (fm Frontmatter) EffectiveTier() int {
	if fm.Tier == 0 {
		return 3
	}
	return fm.Tier
}

// TimeEstimateSource describes where a frame's effective time-prevented
// value came from. Used in `audit analyze` output so the user can see how
// each number was derived.
type TimeEstimateSource string

const (
	TimeFromFrame  TimeEstimateSource = "frame"        // frame frontmatter
	TimeFromConfig TimeEstimateSource = "project-tier" // [time-estimates] override
	TimeFromTier   TimeEstimateSource = "tier-default" // built-in tier default
)

// EffectiveTimeCostHoursPrevented resolves the per-hit hours saved estimate
// for this frame, using the precedence:
//
//  1. The frame's own `time-cost-hours-prevented` field (most specific).
//  2. The project's `[time-estimates] tier-N` override for the frame's tier.
//  3. The built-in DefaultTimeCostHoursPreventedByTier table (least specific).
//
// projectTierOverride should be (value, true) when the project config defines
// an override for this frame's effective tier, else (0, false).
//
// Returns the resolved hours value and the source for transparency in output.
func (fm Frontmatter) EffectiveTimeCostHoursPrevented(projectTierOverride float64, projectTierSet bool) (float64, TimeEstimateSource) {
	if fm.TimeCostHoursPrevented > 0 {
		return fm.TimeCostHoursPrevented, TimeFromFrame
	}
	if projectTierSet {
		return projectTierOverride, TimeFromConfig
	}
	tier := fm.EffectiveTier()
	if tier >= 1 && tier <= 6 {
		return DefaultTimeCostHoursPreventedByTier[tier], TimeFromTier
	}
	return 0, TimeFromTier
}

// Frame is a parsed frame: machine-readable frontmatter + human-readable body.
type Frame struct {
	Frontmatter Frontmatter
	Body        string // markdown body (after the closing `---`)
	SourcePath  string // for error messages and dedup (stdlib path or project path)
}

// ID returns the unique frame identifier, "<category>/<name>".
func (f Frame) ID() string {
	return string(f.Frontmatter.Category) + "/" + f.Frontmatter.Name
}
