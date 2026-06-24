// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

// LinterStarter is one entry in the dashboard's "Start from a pattern"
// dropdown on the Custom linters form. Operators pick a starter, the form
// auto-fills Name / Patterns / Regex / Severity, then they refine (file
// globs, regex tweaks, severity changes) before saving.
//
// Pattern selection rationale: each starter is a SHAPE the operator
// customizes per repo, not a finished check. If a starter regex would be
// universally correct without modification, it should ship as a frame
// instead. Starters intentionally bias toward catching with a couple of
// false positives (the operator filters via globs) rather than missing
// real hits.
type LinterStarter struct {
	ID          string // form field for the JS auto-fill onchange handler
	Label       string // dropdown option text shown to operators
	Description string // one-liner hint about what the regex catches
	Name        string // suggested linter name (lowercase-dashes)
	Patterns    string // suggested file-glob list (comma-separated)
	Regex       string // the regex itself
	Severity    string // WARN / INFO / BLOCK suggestion
}

// LinterStarters is the shipped starter library. Vetted patterns with
// low-enough false-positive rates for the suggested glob; operator
// expected to tighten globs and regex per repo. Adding entries: prefer
// patterns that catch a recurring real concern (secrets, debug leaks,
// reachability fragility) over decorative ones. ~10 entries is the
// sweet spot - enough to cover common needs without overwhelming.
var LinterStarters = []LinterStarter{
	{
		ID:          "urls-in-source",
		Label:       "URLs in source",
		Description: "Catches http/https URLs anywhere in source, useful for finding hardcoded service endpoints that should live in config.",
		Name:        "hardcoded-urls",
		Patterns:    "*.go, *.ts, *.js, *.py",
		Regex:       `https?://\S+`,
		Severity:    "WARN",
	},
	{
		ID:          "todo-markers",
		Label:       "TODO / FIXME / XXX / HACK markers",
		Description: "Any TODO-family marker. Refine to require a date (YYYY-MM-DD) or owner (@name) by tightening the regex; pair with disable markers on legitimate ones.",
		Name:        "todo-markers",
		Patterns:    "*",
		Regex:       `\b(?:TODO|FIXME|XXX|HACK)\b`,
		Severity:    "WARN",
	},
	{
		ID:          "aws-access-key",
		Label:       "AWS access key prefix (AKIA…)",
		Description: "Looks for AKIA-prefixed 20-character strings, the AWS access key ID format. False positives possible in test fixtures.",
		Name:        "aws-access-key",
		Patterns:    "*",
		Regex:       `\bAKIA[0-9A-Z]{16}\b`,
		Severity:    "BLOCK",
	},
	{
		ID:          "stripe-live-key",
		Label:       "Stripe live key prefix (sk_live_…)",
		Description: "Catches sk_live_ prefixed Stripe secret keys. Test keys (sk_test_) intentionally excluded, they're fine to commit.",
		Name:        "stripe-live-key",
		Patterns:    "*",
		Regex:       `\bsk_live_[A-Za-z0-9]{24,}\b`,
		Severity:    "BLOCK",
	},
	{
		ID:          "github-token",
		Label:       "GitHub PAT / token prefix (ghp_, gho_, ghs_, ghu_)",
		Description: "Catches any GitHub-prefixed token (personal, OAuth, server, user-to-server). 36+ chars after prefix.",
		Name:        "github-token",
		Patterns:    "*",
		Regex:       `\bgh[pousr]_[A-Za-z0-9]{36,}\b`,
		Severity:    "BLOCK",
	},
	{
		ID:          "openai-key",
		Label:       "OpenAI / OpenAI-shaped API key (sk-…)",
		Description: "Catches sk- prefixed 20+ character keys: OpenAI, Anthropic, and several other AI provider key formats.",
		Name:        "ai-provider-key",
		Patterns:    "*",
		Regex:       `\bsk-[A-Za-z0-9_-]{20,}\b`,
		Severity:    "BLOCK",
	},
	{
		ID:          "jwt-shape",
		Label:       "JWT token shape (eyJ…\\.…\\.…)",
		Description: "Three base64url segments separated by dots, starting with eyJ (decoded: {). Catches accidentally-committed JWTs.",
		Name:        "jwt-committed",
		Patterns:    "*",
		Regex:       `\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`,
		Severity:    "BLOCK",
	},
	{
		ID:          "localhost-url",
		Label:       "localhost / 127.0.0.1 in URLs",
		Description: "URLs pointing at localhost, useful for finding dev-only endpoints accidentally left in shipped code.",
		Name:        "localhost-in-source",
		Patterns:    "*.go, *.ts, *.js, *.py",
		Regex:       `https?://(?:localhost|127\.0\.0\.1)\b`,
		Severity:    "WARN",
	},
	{
		ID:          "console-log",
		Label:       "Debug log leaks (console.log, fmt.Println, print)",
		Description: "Common debug-print statements. Mostly false-positive-free in shipped code; expect some hits in test files (tighten the glob).",
		Name:        "debug-log-leak",
		Patterns:    "*.ts, *.js, *.go, *.py",
		Regex:       `\b(?:console\.log|fmt\.Println|System\.out\.println|print\()`,
		Severity:    "INFO",
	},
	{
		ID:          "hardcoded-ipv4",
		Label:       "Hardcoded IPv4 address",
		Description: "Matches dotted-quad IPs anywhere in source. Many false positives in test data / config samples, tighten globs.",
		Name:        "hardcoded-ipv4",
		Patterns:    "*.go, *.ts, *.js, *.py",
		Regex:       `\b(?:\d{1,3}\.){3}\d{1,3}\b`,
		Severity:    "INFO",
	},
}
