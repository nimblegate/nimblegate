// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"fmt"
	"path"

	"nimblegate/internal/engine"
)

// zeroRev is git's all-zeros object id, used as oldrev (create) or newrev (delete).
const zeroRev = "0000000000000000000000000000000000000000"

// RefUpdate is one line from a pre/post-receive hook's stdin: "<old> <new> <ref>".
type RefUpdate struct {
	Name   string // full ref, e.g. "refs/heads/main"
	OldRev string
	NewRev string
}

// IsDelete reports whether this update deletes the ref.
func (r RefUpdate) IsDelete() bool { return r.NewRev == zeroRev }

// Policy is the gateway-held configuration for one registered repo.
type Policy struct {
	Repo          string   // logical name (also the bare-repo + config dir name)
	UpstreamURL   string   // where accepted pushes are relayed
	ProtectedRefs []string // glob patterns over full ref names; gated if matched
	GateAllRefs   bool     // true → gate EVERY ref (fail-closed on all branches), ignoring ProtectedRefs
	Enabled       bool     // false → pure pass-through (relay, no gate)
	Observe       bool     // true → check + record findings but never reject; relay anyway (advisory mode)
	PolicyDir     string   // dir holding the gateway-held appframes.toml (+ optional .appframes/)

	// MaxInputSize is the pack-file size cap applied to git-receive-pack via
	// `git config receive.maxInputSize`. Format: empty (no cap), "0" (no cap),
	// "<N>" (bytes), "<N>k"/"m"/"g" (kilo/mega/giga). DefaultReceiveMaxInputSize
	// applies when this is "" AND the repo is newly registered. Operators with
	// LFS-backed binary-heavy repos can leave it small (LFS bypasses the
	// gateway anyway); operators without LFS may need to raise it for
	// legitimate large pushes.
	MaxInputSize string

	// Notification carries the parsed [notification.*] config when present in
	// gateway.toml. Nil = absent section = rail disabled (callers must nil-check).
	// UpstreamKind is NOT populated here; the orchestrator's Registry.LookupByURL
	// derives it from UpstreamURL and writes it onto Notification at wiring time.
	Notification *NotificationConfig
}

// Finding is one non-passing check result captured for observability - recorded
// regardless of severity (BLOCK/ERROR reject; WARN/INFO are advisory).
type Finding struct {
	ID       string `json:"id"`       // frame/linter id, e.g. app-correctness/no-owner-todos
	Severity string `json:"severity"` // BLOCK | ERROR | WARN | INFO
	Message  string `json:"message,omitempty"`
}

// Decision is the gate verdict for a whole push (all-or-nothing).
type Decision struct {
	Accept   bool
	Messages []string  // human-readable lines printed back to the dev on reject
	Findings []Finding // every non-pass result (all severities) for observability
}

// Validate checks the policy is well-formed. It smoke-tests every ProtectedRefs
// glob with path.Match so a malformed pattern is caught at load time rather than
// silently failing to gate at push time (fail-open). Returns the first error.
func (p Policy) Validate() error {
	for _, pat := range p.ProtectedRefs {
		if _, err := path.Match(pat, ""); err != nil {
			return fmt.Errorf("invalid protected-ref pattern %q: %w", pat, err)
		}
	}
	return nil
}

// Checker runs the nimblegate check against a checked-out tree and returns the
// results (after the gateway-held whitelist is applied) plus the list of
// whitelist suppressions, so the gateway can record each one in the audit log.
// Implemented in the commands layer (wired with BuiltinCheckFuncs) and injected,
// so internal/gateway does not import internal/commands.
type Checker interface {
	Check(treeDir string) ([]engine.CheckResult, []engine.SuppressionLog, error)
}

// Suppression is one whitelist-suppressed finding, recorded so a relayed push is
// never a silent exemption. Frame+File+Label identify it; the operator's reason
// lives in the gateway-held whitelist.toml.
type Suppression struct {
	Frame string `json:"frame"`
	File  string `json:"file"`
	Label string `json:"label,omitempty"`
}
