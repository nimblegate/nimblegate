// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// The privilege-separated relay runs as its own account, so every file it
// shares with the dashboard is a permission that nothing verified: a gateway
// whose gateway.toml went owner-only relayed nothing at all, while the Relay
// check blamed the upstream credential. These checks answer the question the
// relay user would ask - can I read what I need, can I write where I must -
// from the mode bits, without needing to be that user.

// Permission bits, as returned by accessBits.
const (
	accessRead  os.FileMode = 4
	accessWrite os.FileMode = 2
	accessExec  os.FileMode = 1
)

// accessBits returns the rwx bits that apply to uid (a member of gids) for fi,
// following the POSIX rule that the first matching class wins outright: an
// owner with no owner bits is denied even when the group bits would allow it.
func accessBits(fi os.FileInfo, uid uint32, gids map[uint32]bool) os.FileMode {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0
	}
	p := fi.Mode().Perm()
	switch {
	case st.Uid == uid:
		return (p >> 6) & 7
	case gids[st.Gid]:
		return (p >> 3) & 7
	default:
		return p & 7
	}
}

// relaySocketFromHook returns the relay socket path baked into the repo's
// post-receive hook, or "" when the repo relays inline (no separate account,
// so nothing to check).
func relaySocketFromHook(bareDir string) string {
	b, err := os.ReadFile(filepath.Join(bareDir, "hooks", "post-receive"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		rest, ok := strings.CutPrefix(strings.TrimSpace(line), "export NBG_RELAY_SOCKET=")
		if !ok {
			continue
		}
		if path, err := strconv.Unquote(rest); err == nil {
			return path
		}
		return rest
	}
	return ""
}

// relayIdentity is the account the relay runs as. Preferably taken from the
// owner of the socket it created (an observation); failing that, from the
// backstop unit's User=, which is the only way to learn the account before the
// backstop has ever written anything.
type relayIdentity struct {
	uid     uint32
	gids    map[uint32]bool
	sockGID uint32 // the socket's own group: the one gid known before any lookup
	name    string // login name, or "uid N" when the account has no passwd entry
}

func relayUserFromSocket(path string) (relayIdentity, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return relayIdentity{}, err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return relayIdentity{}, fmt.Errorf("cannot read ownership of %s", path)
	}
	id := relayIdentity{uid: st.Uid, gids: map[uint32]bool{st.Gid: true}, sockGID: st.Gid, name: "uid " + strconv.FormatUint(uint64(st.Uid), 10)}
	u, err := user.LookupId(strconv.FormatUint(uint64(st.Uid), 10))
	if err != nil {
		return id, nil // no passwd entry: the socket's own group is all we know
	}
	id.name = u.Username
	groups, err := u.GroupIds()
	if err != nil {
		return id, nil
	}
	for _, g := range groups {
		if n, err := strconv.ParseUint(g, 10, 32); err == nil {
			id.gids[uint32(n)] = true
		}
	}
	return id, nil
}

// relayUserFromUnit reads the account a systemd unit runs as. The reconcile
// backstop runs as that user whatever the repo hooks say, so a gateway whose
// pushes relay inline still has a second account that must reach these files.
func relayUserFromUnit(path string) (relayIdentity, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return relayIdentity{}, err
	}
	name := ""
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "User="); ok {
			name = strings.Trim(strings.TrimSpace(v), `"`)
		}
	}
	if name == "" {
		return relayIdentity{}, fmt.Errorf("%s names no User=", path)
	}
	u, err := user.Lookup(name)
	if err != nil {
		return relayIdentity{}, err
	}
	uid, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		return relayIdentity{}, err
	}
	gid, err := strconv.ParseUint(u.Gid, 10, 32)
	if err != nil {
		return relayIdentity{}, err
	}
	id := relayIdentity{uid: uint32(uid), gids: map[uint32]bool{uint32(gid): true}, sockGID: uint32(gid), name: name}
	if groups, err := u.GroupIds(); err == nil {
		for _, g := range groups {
			if n, err := strconv.ParseUint(g, 10, 32); err == nil {
				id.gids[uint32(n)] = true
			}
		}
	}
	return id, nil
}

// relayAccount resolves which account has to reach this repo's policy files.
// The socket baked into post-receive wins when a repo routes its pushes through
// the relay service; otherwise the backstop's unit answers, because a reconcile
// running as another user reads the same files even when every push relays
// inline. That case is the one that bit a live gateway: hooks with no socket,
// so nothing looked privilege-separated, while the backstop silently delivered
// nothing for months.
func relayAccount(profile InstallProfile, bareDir string) (relayIdentity, string, error) {
	if sock := relaySocketFromHook(bareDir); sock != "" {
		id, err := relayUserFromSocket(sock)
		return id, sock, err
	}
	if profile.HasRelayBackstop && profile.RelayUnit != "" {
		id, err := relayUserFromUnit(profile.RelayUnit)
		return id, profile.RelayUnit, err
	}
	return relayIdentity{}, "", nil
}

// doctorCheckRelayAccess verifies the relay user can reach the two files it
// reads and the directory it writes. Silent where one account owns both sides
// (no relay socket in the hook and no backstop unit), since there is then no
// boundary to get wrong.
func doctorCheckRelayAccess(add func(DoctorCheck), cfg DoctorConfig, repo, bareDir string) {
	policyRoot := cfg.PolicyRoot
	id, from, err := relayAccount(cfg.Profile, bareDir)
	if from == "" {
		return
	}
	if err != nil {
		add(DoctorCheck{Repo: repo, Name: "Relay access", Status: DoctorWarn,
			Reason: fmt.Sprintf("the relay account cannot be determined from %s: %v", from, err),
			Fix:    "check the relay service (systemctl status nimblegate-relay); until its account is known, nothing verifies it can read this repo's policy"})
		return
	}

	dir := filepath.Join(policyRoot, repo)
	targets := []struct {
		path     string
		need     os.FileMode
		what     string
		optional bool
	}{
		{dir, accessExec | accessWrite, "write relay-status.json into", false},
		{filepath.Join(dir, "gateway.toml"), accessRead, "read the upstream URL from", false},
		{filepath.Join(dir, "credential"), accessRead, "read the upstream credential from", true},
	}
	var problems, fixes []string
	for _, t := range targets {
		fi, err := os.Stat(t.path)
		if err != nil {
			if !t.optional {
				problems = append(problems, fmt.Sprintf("%s is missing", t.path))
			}
			continue
		}
		if got := accessBits(fi, id.uid, id.gids); got&t.need != t.need {
			problems = append(problems, fmt.Sprintf("cannot %s %s (mode %o)", t.what, t.path, fi.Mode().Perm()))
			fixes = append(fixes, relayAccessFix(fi, t.path, id))
		}
	}
	if len(problems) > 0 {
		add(DoctorCheck{Repo: repo, Name: "Relay access", Status: DoctorFail,
			Reason: fmt.Sprintf("the relay runs as %s and %s; the gate accepts pushes it cannot deliver", id.name, strings.Join(problems, "; ")),
			Fix:    strings.Join(fixes, " && ")})
		return
	}
	add(DoctorCheck{Repo: repo, Name: "Relay access", Status: DoctorOK,
		Reason: fmt.Sprintf("relay user %s can read the policy and credential and write the status file", id.name)})
}

// relayAccessFix names the command that grants the missing access: the group
// path when the relay is already in the file's group, otherwise the regroup
// that has to happen first.
func relayAccessFix(fi os.FileInfo, path string, id relayIdentity) string {
	mode := "g+r"
	if fi.IsDir() {
		mode = "g+ws"
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if ok && id.gids[st.Gid] {
		return fmt.Sprintf("chmod %s %s", mode, path)
	}
	group := strconv.FormatUint(uint64(id.sockGID), 10)
	if g, err := user.LookupGroupId(group); err == nil {
		group = g.Name
	}
	return fmt.Sprintf("chgrp %s %s && chmod %s %s", group, path, mode, path)
}
