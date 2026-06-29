// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"flag"
	"fmt"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
	"nimblegate/internal/gateway"
	"nimblegate/internal/linters"
	"nimblegate/internal/paths"
	"nimblegate/internal/stdlib"
	"nimblegate/internal/version"
	"nimblegate/internal/whitelist"
)

// engineChecker implements gateway.Checker by running the nimblegate engine
// (frames + linters) against a checked-out tree, reusing BuiltinCheckFuncs.
// Headless mirror of Check() (check.go): returns raw results, no formatting.
// The gateway runs a full-tree CLI-equivalent check (TriggerCLI + nil ChangedFiles).
// Applies the gateway-held whitelist (overlaid into the tree); suppressed findings are returned separately so the gateway records each in the audit log.
type engineChecker struct{}

func (engineChecker) Check(root string) ([]engine.CheckResult, []engine.SuppressionLog, error) {
	stdlibFrames, err := stdlib.Load()
	if err != nil {
		return nil, nil, fmt.Errorf("load stdlib: %w", err)
	}
	projectFrames, _ := frames.LoadFromDir(paths.AppframesDir(root))
	gatedStdlib, _ := filterGated(stdlibFrames)
	gatedProject, _ := filterGated(projectFrames)

	e, err := engine.New(engine.Options{
		ProjectRoot:   root,
		StdlibFrames:  gatedStdlib,
		ProjectFrames: gatedProject,
		CheckFuncs:    BuiltinCheckFuncs(),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("engine init: %w", err)
	}
	defer e.Close()

	ctx := engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		WorkingDir:   root,
		ExcludedDirs: e.ExcludedDirs(),
		IgnorePath:   e.IgnorePathFunc(),
	}
	results := engine.Run(e.Registry, ctx)
	lintResults, _ := linters.RunEnabled(e.ProjectConfig.Linters, root, e.ExcludedDirs())
	results = append(results, lintResults...)

	// Apply the gateway-held whitelist (overlaid into the tree at
	// .appframes/_canonical/whitelist.toml). Missing file → empty whitelist, no
	// error → nothing suppressed. A malformed whitelist returns an error and
	// fails the check loudly (mirrors local Check) - never a silent exemption.
	//
	// Validate frame IDs against the FULL stdlib + project catalog (not just
	// the post-enablement engine registry) - the whitelist is allowed to
	// reference frames that aren't currently enabled (they may get enabled
	// later via kit apply, and the suppression should already be in place).
	wl, err := whitelist.LoadFromProject(root, allKnownIDsWithLinters(stdlibFrames, projectFrames, e), time.Now().UTC())
	if err != nil {
		return nil, nil, fmt.Errorf("whitelist: %w", err)
	}
	filtered, suppressed := engine.ApplyWhitelist(results, wl, root)
	return filtered, suppressed, nil
}

// Gateway dispatches `nimblegate gateway <sub>`.
func Gateway(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: nimblegate gateway <add|archive|delete|restore|rescan|migrate-layout|pre-receive|post-receive|relay-service|harden-sshd|shell|access|doctor|dashboard|setup-token|analytics|token|bind|tls-setup|benchmark> ...")
		return 2
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "add":
		return gatewayAdd(rest)
	case "archive":
		return gatewayArchive(rest)
	case "delete":
		return gatewayDelete(rest)
	case "restore":
		return gatewayRestore(rest)
	case "rescan":
		return gatewayRescan(rest)
	case "migrate-layout":
		return gatewayMigrateLayout(rest)
	case "pre-receive":
		return gatewayPreReceive(rest)
	case "post-receive":
		return gatewayPostReceive(rest)
	case "dashboard":
		return gatewayDashboard(rest)
	case "setup-token":
		return gatewaySetupToken(rest)
	case "analytics":
		return gatewayAnalytics(rest)
	case "token":
		return gatewayToken(rest)
	case "bind":
		return gatewayBind(rest)
	case "tls-setup":
		return gatewayTLSSetup(rest)
	case "benchmark":
		return gatewayBenchmark(rest)
	case "relay-service":
		return gatewayRelayService(rest)
	case "harden-sshd":
		return gatewayHardenSSHD(rest)
	case "shell":
		return gatewayGitShell(rest)
	case "access":
		return gatewayAccess(rest)
	case "doctor":
		return gatewayDoctor(rest)
	default:
		fmt.Fprintf(os.Stderr, "nimblegate gateway: unknown subcommand %q\n", sub)
		return 2
	}
}

func gatewayAdd(args []string) int {
	fs := flag.NewFlagSet("gateway add", flag.ExitOnError)
	name := fs.String("name", "", "logical repo name")
	upstream := fs.String("upstream", "", "true upstream URL to relay accepted pushes to")
	protect := fs.String("protect", "refs/heads/*", "comma-separated protected ref globs")
	policyRoot := fs.String("policy-root", "/etc/nimblegate-gateway/repos", "per-repo config dir root")
	reposRoot := fs.String("repos-root", "/srv/nimblegate-gateway/repos", "bare-repo root")
	disabled := fs.Bool("disabled", false, "register but do not gate (pass-through)")
	gateAllRefs := fs.Bool("gate-all-refs", false, "gate EVERY ref (fail-closed on all branches), not just --protect; default off (protected refs only)")
	observe := fs.Bool("observe", false, "advisory mode: check + record findings but never reject (relay anyway)")
	noImport := fs.Bool("no-import", false, "skip mirroring the upstream's existing history at registration")
	relaySocket := fs.String("relay-socket", "", "if set, route relay through the privilege-separated relay service at this Unix socket (bakes NBG_RELAY_SOCKET into the post-receive hook); empty = legacy inline relay")
	_ = fs.Parse(args)
	if *name == "" || *upstream == "" {
		fmt.Fprintln(os.Stderr, "gateway add: --name and --upstream are required")
		return 2
	}
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "gateway add: cannot resolve own executable path: %v\n", err)
		return 1
	}
	if *relaySocket != "" && gateway.UpstreamURLHasEmbeddedToken(*upstream) {
		fmt.Fprintf(os.Stderr, "gateway add: WARNING: the upstream URL embeds a credential, but --relay-socket requires the\n"+
			"  credential to live ONLY in %s/%s/credential (gateway.toml is git-readable for gating,\n"+
			"  so a token in the URL is a bypass). Use a tokenless upstream URL + the credential file.\n",
			*policyRoot, *name)
	}
	if err := gateway.AddRepo(gateway.AddOptions{
		Name:          *name,
		UpstreamURL:   *upstream,
		ProtectedRefs: splitComma(*protect),
		GateAllRefs:   *gateAllRefs,
		Enabled:       !*disabled,
		Observe:       *observe,
		PolicyRoot:    *policyRoot,
		ReposRoot:     *reposRoot,
		SelfExe:       exe,
		RelaySocket:   *relaySocket,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "gateway add: %v\n", err)
		return 1
	}
	fmt.Printf("registered %q → %s (bare repo under %s)\n", *name, *upstream, *reposRoot)
	fmt.Printf("install the upstream credential at %s/%s/credential (0600) if needed\n", *policyRoot, *name)

	// Mirror the upstream's existing history so the repo is immediately
	// clone-able from the gateway. No-op for an empty upstream. SSH upstreams
	// auth via the host key; for an http upstream whose credential isn't set
	// yet, the import is skipped and recoverable via Sync on /repos.
	if !*noImport {
		cred, _ := gateway.FileCredentialStore{Root: *policyRoot}.Load(*name)
		res, serr := gateway.SeedAtRegistration(*policyRoot, *reposRoot, *name, *upstream, cred)
		switch {
		case serr != nil:
			fmt.Fprintf(os.Stderr, "note: could not mirror upstream history (%v)\n      set the credential and run Sync from /repos, or re-run with the upstream reachable\n", serr)
		case res.Refs > 0:
			fmt.Printf("mirrored %d branch(es) from upstream (default branch %q)\n", res.Refs, res.DefaultBranch)
		}
	}
	return 0
}

func gatewayPreReceive(args []string) int {
	repo, policyRoot := gatewayHookFlags(args)
	pol, err := gateway.FilePolicyStore{Root: policyRoot}.Load(repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gateway pre-receive: %v\n", err)
		return 1
	}
	host, _ := os.Hostname()
	return gateway.RunPreReceive(gateway.PreReceiveDeps{
		Policy:    pol,
		GitDir:    gitDirFromEnv(),
		Checker:   engineChecker{},
		AuditPath: filepath.Join(policyRoot, repo, "audit.log"),
		// Notification rail: pol.Notification is the resolved config loaded from
		// gateway.toml. Without these two, fireNotification's guard
		// (NotificationConfig != nil) is false and no queue record is ever
		// written - the rail silently never fires. PolicyRoot locates the queue
		// file the dashboard daemon drains.
		NotificationConfig: pol.Notification,
		PolicyRoot:         policyRoot,
		GatewayVersion:     version.Version,
		InstanceID:         host,
	}, os.Stdin, os.Stderr) // stderr → git surfaces it as remote: lines
}

func gatewayPostReceive(args []string) int {
	repo, policyRoot := gatewayHookFlags(args)
	pol, err := gateway.FilePolicyStore{Root: policyRoot}.Load(repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gateway post-receive: %v\n", err)
		return 1
	}
	// In relay-service mode the credential is held by the relay user, not git -
	// so this (git-side) process must NOT read it. Only load the credential for
	// the legacy inline-relay path (no socket configured).
	relaySocket := os.Getenv("NBG_RELAY_SOCKET")
	var cred string
	if relaySocket == "" {
		cred, _ = gateway.FileCredentialStore{Root: policyRoot}.Load(repo)
	}
	gitDir := gitDirFromEnv()
	// git invokes hooks with GIT_DIR="." (relative) and cwd inside the bare;
	// absolute-ize at the entry so downstream consumers (ScanFirstPush, ReposRoot
	// derivation) don't resolve against a now-irrelevant cwd.
	if abs, err := filepath.Abs(gitDir); err == nil {
		gitDir = abs
	}
	// Derive ReposRoot from the bare repo path (one level up from <repo>.git);
	// this avoids needing a --repos-root flag in the hook script, so repos
	// installed before scan-on-first-push existed still get the behavior.
	reposRoot := filepath.Dir(gitDir)
	exe, _ := os.Executable()
	return gateway.RunPostReceive(gateway.PostReceiveDeps{
		Policy:      pol,
		GitDir:      gitDir,
		Cred:        cred,
		Repo:        repo,
		PolicyRoot:  policyRoot,
		ReposRoot:   reposRoot,
		SelfExe:     exe,
		RelaySocket: relaySocket,
	}, os.Stdin, os.Stderr)
}

// gatewayRelayService runs the privilege-separated relay: a long-lived process
// (started as the dedicated relay user) that owns the upstream credential and
// accepts credential-free relay jobs from the git-side post-receive hook over a
// local Unix socket. git connects but cannot read the credential. The socket is
// locked to owner+group (0660); --socket-group lets the git user's group connect.
func gatewayRelayService(args []string) int {
	fs := flag.NewFlagSet("gateway relay-service", flag.ExitOnError)
	policyRoot := fs.String("policy-root", "/etc/nimblegate-gateway/repos", "per-repo config dir root")
	reposRoot := fs.String("repos-root", "/srv/nimblegate-gateway/repos", "bare-repo root")
	socket := fs.String("socket", "/run/nimblegate/relay.sock", "Unix socket to accept relay jobs on")
	socketGroup := fs.String("socket-group", "", "group allowed to connect (the git user's group); empty leaves the default")
	reconcileEvery := fs.Duration("reconcile-interval", 5*time.Minute, "how often to re-push drift to upstream (recovers pushes accepted while the service was down); 0 disables")
	_ = fs.Parse(args)

	if err := os.MkdirAll(filepath.Dir(*socket), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "gateway relay-service: socket dir: %v\n", err)
		return 1
	}
	if err := os.Remove(*socket); err != nil && !os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "gateway relay-service: clear stale socket: %v\n", err)
		return 1
	}
	ln, err := net.Listen("unix", *socket)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gateway relay-service: listen %s: %v\n", *socket, err)
		return 1
	}
	defer ln.Close()
	// Lock the socket: owner (relay user) + group only, no world access.
	if err := os.Chmod(*socket, 0o660); err != nil {
		fmt.Fprintf(os.Stderr, "gateway relay-service: chmod socket: %v\n", err)
		return 1
	}
	if *socketGroup != "" {
		g, err := user.LookupGroup(*socketGroup)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gateway relay-service: group %q: %v\n", *socketGroup, err)
			return 1
		}
		gid, _ := strconv.Atoi(g.Gid)
		if err := os.Chown(*socket, -1, gid); err != nil {
			fmt.Fprintf(os.Stderr, "gateway relay-service: chown socket group: %v\n", err)
			return 1
		}
	}
	fmt.Fprintf(os.Stderr, "nimblegate gateway relay-service: listening on %s (policy-root=%s repos-root=%s)\n", *socket, *policyRoot, *reposRoot)
	// Reconciler backstop: periodically re-push any ref the upstream is missing
	// or behind (recovers pushes accepted while the service was down).
	if *reconcileEvery > 0 {
		go func() {
			t := time.NewTicker(*reconcileEvery)
			defer t.Stop()
			for range t.C {
				if n, err := gateway.ReconcileAll(*reposRoot, *policyRoot); err == nil && n > 0 {
					fmt.Fprintf(os.Stderr, "nimblegate gateway relay-service: reconciled %d drifted ref(s)\n", n)
				}
			}
		}()
	}
	svc := &gateway.RelayService{Resolve: gateway.NewRepoResolver(*reposRoot, *policyRoot)}
	if err := svc.Serve(ln); err != nil {
		fmt.Fprintf(os.Stderr, "gateway relay-service: %v\n", err)
		return 1
	}
	return 0
}

func gatewayHookFlags(args []string) (repo, policyRoot string) {
	fs := flag.NewFlagSet("gateway hook", flag.ExitOnError)
	r := fs.String("repo", "", "repo name")
	pr := fs.String("policy-root", "/etc/nimblegate-gateway/repos", "policy root")
	_ = fs.Parse(args)
	return *r, *pr
}

// gitDirFromEnv returns the bare repo dir git sets for hooks ($GIT_DIR), or ".".
func gitDirFromEnv() string {
	if d := os.Getenv("GIT_DIR"); d != "" {
		return d
	}
	return "."
}

func splitComma(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// gatewayArchive removes both activation symlinks (files in _repos/ untouched),
// logs the event, and regenerates _archived.md from the events log - CLI mirror
// of repoLifecycleHandlers.archive.
func gatewayArchive(args []string) int {
	fs := flag.NewFlagSet("gateway archive", flag.ExitOnError)
	name := fs.String("name", "", "repo name")
	policyRoot := fs.String("policy-root", "/etc/nimblegate-gateway/repos", "policy root")
	reposRoot := fs.String("repos-root", "/srv/nimblegate-gateway/repos", "repos root")
	_ = fs.Parse(args)
	if *name == "" {
		fmt.Fprintln(os.Stderr, "gateway archive: --name required")
		return 2
	}
	if err := gateway.ArchiveRepo(gateway.ArchiveOptions{
		Name: *name, PolicyRoot: *policyRoot, ReposRoot: *reposRoot,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "gateway archive: %v\n", err)
		return 2
	}
	_ = gateway.AppendEvent(*policyRoot, gateway.Event{
		Event: "archive", Repo: *name, OK: true,
	})
	_ = gateway.RegenerateArchivedMarkdown(*policyRoot)
	fmt.Printf("archived %s: files preserved under %s/_repos/%s/\n", *name, *policyRoot, *name)
	return 0
}

// gatewayDelete PERMANENTLY removes a repo's whole footprint (bare repo + the
// policy/audit/credential dir). Irreversible, so it requires an explicit --yes;
// without it, it refuses and points at `archive` (the reversible option).
func gatewayDelete(args []string) int {
	fs := flag.NewFlagSet("gateway delete", flag.ExitOnError)
	name := fs.String("name", "", "repo name")
	policyRoot := fs.String("policy-root", "/etc/nimblegate-gateway/repos", "policy root")
	reposRoot := fs.String("repos-root", "/srv/nimblegate-gateway/repos", "repos root")
	yes := fs.Bool("yes", false, "confirm permanent, irreversible deletion (required)")
	_ = fs.Parse(args)
	if *name == "" {
		fmt.Fprintln(os.Stderr, "gateway delete: --name required")
		return 2
	}
	if !*yes {
		fmt.Fprintf(os.Stderr, "gateway delete: refusing to permanently delete %q without --yes.\n", *name)
		fmt.Fprintln(os.Stderr, "  This erases the bare repo (ALL git history) AND the policy/audit/credential dir, irreversible.")
		fmt.Fprintln(os.Stderr, "  To keep the files, use `nimblegate gateway archive` instead.")
		return 2
	}
	if err := gateway.DeleteRepo(gateway.DeleteOptions{
		Name: *name, PolicyRoot: *policyRoot, ReposRoot: *reposRoot,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "gateway delete: %v\n", err)
		return 1
	}
	_ = gateway.AppendEvent(*policyRoot, gateway.Event{
		Event: "delete", Repo: *name, OK: true,
	})
	_ = gateway.RegenerateArchivedMarkdown(*policyRoot)
	fmt.Printf("permanently deleted %s: removed %s/_repos/%s/ and %s/_repos/%s.git/\n",
		*name, *policyRoot, *name, *reposRoot, *name)
	return 0
}

// gatewayRestore re-creates both activation symlinks, logs the event, and
// refreshes _archived.md - CLI mirror of repoLifecycleHandlers.restore.
func gatewayRestore(args []string) int {
	fs := flag.NewFlagSet("gateway restore", flag.ExitOnError)
	name := fs.String("name", "", "repo name")
	policyRoot := fs.String("policy-root", "/etc/nimblegate-gateway/repos", "policy root")
	reposRoot := fs.String("repos-root", "/srv/nimblegate-gateway/repos", "repos root")
	_ = fs.Parse(args)
	if *name == "" {
		fmt.Fprintln(os.Stderr, "gateway restore: --name required")
		return 2
	}
	if err := gateway.RestoreRepo(gateway.RestoreOptions{
		Name: *name, PolicyRoot: *policyRoot, ReposRoot: *reposRoot,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "gateway restore: %v\n", err)
		return 2
	}
	_ = gateway.AppendEvent(*policyRoot, gateway.Event{
		Event: "restore", Repo: *name, OK: true,
	})
	_ = gateway.RegenerateArchivedMarkdown(*policyRoot)
	fmt.Printf("restored %s\n", *name)
	return 0
}

// gatewayRescan deletes the cached recommendation and re-runs the first-push
// scan synchronously - CLI mirror of repoLifecycleHandlers.scanRescan. The
// --self-exe override is for tests; in normal use we resolve our own path.
func gatewayRescan(args []string) int {
	fs := flag.NewFlagSet("gateway rescan", flag.ExitOnError)
	name := fs.String("name", "", "repo name")
	policyRoot := fs.String("policy-root", "/etc/nimblegate-gateway/repos", "policy root")
	reposRoot := fs.String("repos-root", "/srv/nimblegate-gateway/repos", "repos root")
	selfExeFlag := fs.String("self-exe", "", "path to nimblegate binary (defaults to os.Executable())")
	_ = fs.Parse(args)
	if *name == "" {
		fmt.Fprintln(os.Stderr, "gateway rescan: --name required")
		return 2
	}
	selfExe := *selfExeFlag
	if selfExe == "" {
		if exe, err := os.Executable(); err == nil {
			selfExe = exe
		} else {
			fmt.Fprintf(os.Stderr, "gateway rescan: %v\n", err)
			return 2
		}
	}
	recPath := filepath.Join(*policyRoot, *name, "scan-recommendation.json")
	_ = os.Remove(recPath)
	bare := filepath.Join(*reposRoot, *name+".git")
	if err := gateway.ScanFirstPush(bare, *name, *policyRoot, selfExe); err != nil {
		fmt.Fprintf(os.Stderr, "gateway rescan: %v\n", err)
		return 2
	}
	_ = gateway.AppendEvent(*policyRoot, gateway.Event{
		Event: "scan-rescan", Repo: *name, OK: true,
	})
	fmt.Printf("rescanned %s: recommendation at %s\n", *name, recPath)
	return 0
}

// gatewayMigrateLayout is the one-time upgrade step for boxes carrying the
// legacy direct-dir layout. We snapshot the pre-migration repo names so the
// event payload records exactly what was migrated; if there's nothing to do,
// no event is written (an idempotent re-run stays silent).
func gatewayMigrateLayout(args []string) int {
	fs := flag.NewFlagSet("gateway migrate-layout", flag.ExitOnError)
	policyRoot := fs.String("policy-root", "/etc/nimblegate-gateway/repos", "policy root")
	reposRoot := fs.String("repos-root", "/srv/nimblegate-gateway/repos", "repos root")
	_ = fs.Parse(args)
	before := listLegacyRepoNames(*policyRoot)
	if err := gateway.MigrateToSymlinkLayout(gateway.MigrateOptions{
		PolicyRoot: *policyRoot, ReposRoot: *reposRoot,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "gateway migrate-layout: %v\n", err)
		return 2
	}
	if len(before) > 0 {
		_ = gateway.AppendEvent(*policyRoot, gateway.Event{
			Event:   "migrate-layout",
			OK:      true,
			Payload: map[string]any{"migrated": before},
		})
	}
	fmt.Printf("migrated %d repo(s)\n", len(before))
	return 0
}

// listLegacyRepoNames returns names of direct subdirs of policyRoot that are
// real dirs (not symlinks, not internal) - i.e. the pre-migration repos.
func listLegacyRepoNames(policyRoot string) []string {
	ents, err := os.ReadDir(policyRoot)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		name := e.Name()
		if name == "_repos" || strings.HasPrefix(name, "_archive") || strings.HasPrefix(name, "_events") {
			continue
		}
		if !e.IsDir() {
			continue
		}
		full := filepath.Join(policyRoot, name)
		fi, err := os.Lstat(full)
		if err != nil || fi.Mode()&os.ModeSymlink != 0 {
			continue // already migrated
		}
		out = append(out, name)
	}
	return out
}
