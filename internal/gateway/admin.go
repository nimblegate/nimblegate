// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// AddOptions configures registering one repo on the gateway.
type AddOptions struct {
	Name          string
	UpstreamURL   string
	ProtectedRefs []string
	GateAllRefs   bool // gate every ref, not just ProtectedRefs
	Enabled       bool
	Observe       bool   // true → advisory mode (check + record, never reject)
	PolicyRoot    string // where per-repo config dirs live
	ReposRoot     string // where bare repos live
	SelfExe       string // absolute path to the nimblegate binary, baked into hooks
	RelaySocket   string // if set, the post-receive hook exports NBG_RELAY_SOCKET so the relay routes through the privilege-separated service
}

// AddRepo creates the bare repo, installs the pre/post-receive hooks, and saves
// the policy. The credential (if any) is installed separately via the cred store.
func AddRepo(o AddOptions) error {
	if o.Name == "_repos" || strings.HasPrefix(o.Name, "_archive") || strings.HasPrefix(o.Name, "_events") {
		return fmt.Errorf("reserved repo name: %q", o.Name)
	}
	libPolicy := filepath.Join(o.PolicyRoot, "_repos", o.Name)
	libBare := filepath.Join(o.ReposRoot, "_repos", o.Name+".git")
	linkPolicy := filepath.Join(o.PolicyRoot, o.Name)
	linkBare := filepath.Join(o.ReposRoot, o.Name+".git")

	// Refuse if any of the four target paths already exist.
	for _, p := range []string{libPolicy, libBare, linkPolicy, linkBare} {
		if _, err := os.Lstat(p); err == nil {
			return fmt.Errorf("already exists: %s", p)
		}
	}

	// Lib dirs.
	if err := os.MkdirAll(libPolicy, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(libBare), 0o755); err != nil {
		_ = os.RemoveAll(libPolicy)
		return err
	}
	if out, err := exec.Command("git", "init", "--bare", "-q", libBare).CombinedOutput(); err != nil {
		_ = os.RemoveAll(libPolicy)
		return fmt.Errorf("init bare repo: %w\n%s", err, out)
	}

	// Apply receive.maxInputSize to the bare repo so a malicious push can't
	// fill /srv before pre-receive even runs. Default 500 MiB; operator
	// overrides via [max-input-size] in gateway.toml + re-registering or
	// running `git config` manually.
	if err := ApplyReceiveCap(libBare, DefaultReceiveMaxInputSize); err != nil {
		_ = os.RemoveAll(libPolicy)
		_ = os.RemoveAll(libBare)
		return fmt.Errorf("apply receive cap: %w", err)
	}

	// Write gateway.toml directly into the lib path (bypassing
	// FilePolicyStore.Save, which would resolve through the not-yet-existing
	// activation symlink).
	gwPath := filepath.Join(libPolicy, "gateway.toml")
	if err := writeGatewayTOML(gwPath, Policy{
		Repo:          o.Name,
		UpstreamURL:   o.UpstreamURL,
		ProtectedRefs: o.ProtectedRefs,
		GateAllRefs:   o.GateAllRefs,
		Enabled:       o.Enabled,
		Observe:       o.Observe,
		MaxInputSize:  DefaultReceiveMaxInputSize,
	}); err != nil {
		_ = os.RemoveAll(libPolicy)
		_ = os.RemoveAll(libBare)
		return err
	}

	// Seed appframes.toml so the dashboard's frame/kit handlers find a
	// parseable file from the first click. Without this seed, /policy/kits/apply
	// and /policy/frames/toggle visibly no-op on a freshly-registered repo
	// until the first save creates the file - the "click does nothing" trap
	// surfaced during ai-assistant onboarding. Written to the lib path
	// directly since the activation symlink is created later.
	libFramePolicy := filepath.Join(libPolicy, "appframes.toml")
	if err := os.WriteFile(libFramePolicy, defaultAppframesTOML(), 0o644); err != nil {
		_ = os.RemoveAll(libPolicy)
		_ = os.RemoveAll(libBare)
		return fmt.Errorf("seed appframes.toml: %w", err)
	}

	// Install hooks inside the bare.
	for _, hook := range []string{"pre-receive", "post-receive"} {
		// Only post-receive relays; bake the relay socket into its environment
		// so the relay routes through the service (the credential-holding relay
		// user), never inline as git. pre-receive never needs it.
		var env string
		if hook == "post-receive" && o.RelaySocket != "" {
			env = fmt.Sprintf("export NBG_RELAY_SOCKET=%q\n", o.RelaySocket)
		}
		script := fmt.Sprintf("#!/bin/sh\n%sexec %q gateway %s --repo %q --policy-root %q\n",
			env, o.SelfExe, hook, o.Name, o.PolicyRoot)
		if err := os.WriteFile(filepath.Join(libBare, "hooks", hook), []byte(script), 0o755); err != nil {
			_ = os.RemoveAll(libPolicy)
			_ = os.RemoveAll(libBare)
			return err
		}
	}

	// Activation symlinks last (relative targets so the layout survives a root mv).
	if err := os.Symlink(filepath.Join("_repos", o.Name), linkPolicy); err != nil {
		_ = os.RemoveAll(libPolicy)
		_ = os.RemoveAll(libBare)
		return err
	}
	if err := os.Symlink(filepath.Join("_repos", o.Name+".git"), linkBare); err != nil {
		_ = os.Remove(linkPolicy)
		_ = os.RemoveAll(libPolicy)
		_ = os.RemoveAll(libBare)
		return err
	}

	// Match ownership to the existing reposRoot/policyRoot owner so the
	// git user can read what we just created. Common footgun: running
	// `gateway add` via root SSH creates root-owned files, then git-shell
	// (running as the git user) rejects the next push as "dubious
	// ownership." MatchParentOwnership is a no-op when the running user
	// already matches the parent, so this is safe to call even when
	// gateway add is invoked AS the git user already.
	if err := MatchParentOwnership(libBare, o.ReposRoot); err != nil {
		// Don't fail the whole AddRepo - the files exist + symlinks point
		// at them; the operator can chown manually. But surface the
		// problem so they know to fix it before pushing.
		fmt.Fprintf(os.Stderr, "warning: chown bare repo to repos-root owner: %v\n", err)
	}
	if err := MatchParentOwnership(libPolicy, o.PolicyRoot); err != nil {
		fmt.Fprintf(os.Stderr, "warning: chown policy dir to policy-root owner: %v\n", err)
	}
	return nil
}

type MigrateOptions struct {
	PolicyRoot string
	ReposRoot  string
}

// MigrateToSymlinkLayout moves every direct subdir of PolicyRoot (except
// internal entries: _repos, _archive*, _events*, _archived.md) into
// <PolicyRoot>/_repos/<name>/ and creates an activation symlink at
// <PolicyRoot>/<name>. Same for <ReposRoot>/<name>.git/. Idempotent: an entry
// that is already a symlink, or whose _repos/<name> target already exists, is
// skipped. Regular files (e.g. _archived.md, _events.jsonl) are skipped.
func MigrateToSymlinkLayout(o MigrateOptions) error {
	if err := migrateOneRoot(o.PolicyRoot, ""); err != nil {
		return err
	}
	return migrateOneRoot(o.ReposRoot, ".git")
}

func migrateOneRoot(root, suffix string) error {
	if root == "" {
		return nil
	}
	libRoot := filepath.Join(root, "_repos")
	if err := os.MkdirAll(libRoot, 0o755); err != nil {
		return err
	}
	ents, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, e := range ents {
		name := e.Name()
		// Skip internal/system entries.
		if name == "_repos" || strings.HasPrefix(name, "_archive") || strings.HasPrefix(name, "_events") {
			continue
		}
		// Skip non-directories (markdown logs, JSONL, etc.).
		if !e.IsDir() {
			continue
		}
		// When migrating repos-root, only entries matching the .git suffix.
		if suffix != "" && !strings.HasSuffix(name, suffix) {
			continue
		}
		src := filepath.Join(root, name)
		fi, err := os.Lstat(src)
		if err != nil {
			continue
		}
		// Already a symlink → assume migrated.
		if fi.Mode()&os.ModeSymlink != 0 {
			continue
		}
		dst := filepath.Join(libRoot, name)
		// Lib already has this entry from a previous partial run; skip.
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		if err := os.Rename(src, dst); err != nil {
			return fmt.Errorf("migrate %s: %w", name, err)
		}
		if err := os.Symlink(filepath.Join("_repos", name), src); err != nil {
			return fmt.Errorf("symlink %s: %w", name, err)
		}
	}
	return nil
}

type ArchiveOptions struct {
	Name       string
	PolicyRoot string
	ReposRoot  string
}

// ArchiveRepo removes the two activation symlinks (<policyRoot>/<name> and
// <reposRoot>/<name>.git). Files in _repos/ are untouched. Archived state is
// "lib exists, no parent symlink". Rolls back on partial failure.
func ArchiveRepo(o ArchiveOptions) error {
	linkPolicy := filepath.Join(o.PolicyRoot, o.Name)
	linkBare := filepath.Join(o.ReposRoot, o.Name+".git")
	for _, p := range []string{linkPolicy, linkBare} {
		fi, err := os.Lstat(p)
		if err != nil {
			return fmt.Errorf("not active: %s", p)
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			return fmt.Errorf("not a symlink (un-migrated?): %s", p)
		}
	}
	if err := os.Remove(linkPolicy); err != nil {
		return err
	}
	if err := os.Remove(linkBare); err != nil {
		// Roll back the policy symlink so state stays consistent.
		_ = os.Symlink(filepath.Join("_repos", o.Name), linkPolicy)
		return err
	}
	return nil
}

type DeleteOptions struct {
	Name       string
	PolicyRoot string
	ReposRoot  string
}

// DeleteRepo PERMANENTLY removes a repo's entire on-disk footprint - both
// activation symlinks (if still active) AND the real lib dirs under _repos/ in
// each root:
//   - the bare repo  <reposRoot>/_repos/<name>.git  (all git history)
//   - the policy dir <policyRoot>/_repos/<name>     (gateway.toml, appframes.toml,
//     credential, audit.log, scan/access state)
//
// This is irreversible - ArchiveRepo is the reversible alternative (it keeps the
// lib dirs). Works whether the repo is active or already archived (a missing
// symlink is fine). Refuses the reserved names and errors if the repo isn't
// found at all, so a typo'd name can't silently "succeed".
func DeleteRepo(o DeleteOptions) error {
	if o.Name == "" || o.Name == "_repos" || strings.HasPrefix(o.Name, "_archive") || strings.HasPrefix(o.Name, "_events") {
		return fmt.Errorf("refusing to delete reserved/invalid repo name %q", o.Name)
	}
	libPolicy := filepath.Join(o.PolicyRoot, "_repos", o.Name)
	libBare := filepath.Join(o.ReposRoot, "_repos", o.Name+".git")
	_, ep := os.Stat(libPolicy)
	_, eb := os.Stat(libBare)
	if os.IsNotExist(ep) && os.IsNotExist(eb) {
		return fmt.Errorf("repo %q not found (nothing at %s or %s)", o.Name, libPolicy, libBare)
	}
	// Drop the activation symlinks first (best-effort - absent when archived).
	for _, link := range []string{
		filepath.Join(o.PolicyRoot, o.Name),
		filepath.Join(o.ReposRoot, o.Name+".git"),
	} {
		if fi, err := os.Lstat(link); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			_ = os.Remove(link)
		}
	}
	// Then remove the real lib dirs.
	var firstErr error
	for _, dir := range []string{libPolicy, libBare} {
		if err := os.RemoveAll(dir); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

type RestoreOptions struct {
	Name       string
	PolicyRoot string
	ReposRoot  string
}

// RestoreRepo creates activation symlinks back at the parent paths. Errors if
// the lib entry is missing or if an activation symlink already exists.
func RestoreRepo(o RestoreOptions) error {
	libPolicy := filepath.Join(o.PolicyRoot, "_repos", o.Name)
	libBare := filepath.Join(o.ReposRoot, "_repos", o.Name+".git")
	if _, err := os.Stat(libPolicy); err != nil {
		return fmt.Errorf("no lib policy: %w", err)
	}
	if _, err := os.Stat(libBare); err != nil {
		return fmt.Errorf("no lib bare: %w", err)
	}
	linkPolicy := filepath.Join(o.PolicyRoot, o.Name)
	linkBare := filepath.Join(o.ReposRoot, o.Name+".git")
	if _, err := os.Lstat(linkPolicy); err == nil {
		return fmt.Errorf("already active: %s", linkPolicy)
	}
	if _, err := os.Lstat(linkBare); err == nil {
		return fmt.Errorf("already active: %s", linkBare)
	}
	if err := os.Symlink(filepath.Join("_repos", o.Name), linkPolicy); err != nil {
		return err
	}
	if err := os.Symlink(filepath.Join("_repos", o.Name+".git"), linkBare); err != nil {
		_ = os.Remove(linkPolicy)
		return err
	}
	return nil
}

// ListArchivedRepos returns names that exist under <policyRoot>/_repos/ but
// have no activation symlink at <policyRoot>/<name>. Sorted.
func ListArchivedRepos(policyRoot string) []string {
	libRoot := filepath.Join(policyRoot, "_repos")
	ents, err := os.ReadDir(libRoot)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		link := filepath.Join(policyRoot, e.Name())
		if _, err := os.Lstat(link); err == nil {
			continue // active
		}
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}

// IsArchivedRepo reports whether <name> exists as a lib dir under
// <policyRoot>/_repos/ but has no activation symlink at <policyRoot>/<name> -
// i.e. it was archived (files kept, symlinks dropped) rather than active or
// absent. Used to give a Restore/Delete-aware message when AddRepo reports the
// name already exists.
func IsArchivedRepo(policyRoot, name string) bool {
	if name == "" || name == "_repos" {
		return false
	}
	if _, err := os.Stat(filepath.Join(policyRoot, "_repos", name)); err != nil {
		return false
	}
	_, err := os.Lstat(filepath.Join(policyRoot, name))
	return os.IsNotExist(err)
}

// RegenerateArchivedMarkdown rewrites _archived.md from _events.jsonl filtered
// to archive + restore events. Always creates the file (header-only if no
// matching events).
func RegenerateArchivedMarkdown(policyRoot string) error {
	events, err := ReadEvents(policyRoot, func(e Event) bool {
		return e.Event == "archive" || e.Event == "restore"
	})
	if err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("# Gateway repo lifecycle log\n\n")
	b.WriteString("Each archive removes activation symlinks; underlying files stay in `_repos/`.\n")
	b.WriteString("Restore re-creates the symlinks. Nothing is deleted by these operations.\n\n")
	b.WriteString("| Timestamp (UTC)     | Repo       | Action  |\n")
	b.WriteString("|---------------------|------------|---------|\n")
	for _, e := range events {
		b.WriteString(fmt.Sprintf("| %s | %-10s | %-7s |\n",
			e.Timestamp.UTC().Format("2006-01-02 15:04:05"), e.Repo, e.Event))
	}
	return os.WriteFile(filepath.Join(policyRoot, "_archived.md"), []byte(b.String()), 0o644)
}
