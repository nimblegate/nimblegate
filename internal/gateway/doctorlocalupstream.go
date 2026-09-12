// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// Git refuses to use a repository another user owns unless safe.directory
// allows it ("detected dubious ownership"). Whoever relays uses a local
// upstream: the push-time relay runs as the gateway repo's owner, and a
// bare-metal backstop or relay service as its own account. Any of them that
// neither owns the upstream nor is allowed by safe.directory has every relay
// fail with a git error that names neither the gateway nor the fix.
func doctorCheckLocalUpstreamAccess(add func(DoctorCheck), cfg DoctorConfig, repo, upstreamPath string) {
	owner, ok := ownerUID(upstreamPath)
	if !ok {
		return
	}
	bare := filepath.Join(cfg.ReposRoot, repo+".git")
	var uids []uint32
	if uid, ok := ownerUID(bare); ok {
		uids = append(uids, uid)
	}
	if id, from, err := relayAccount(cfg.Profile, bare); from != "" && err == nil && (len(uids) == 0 || id.uid != uids[0]) {
		uids = append(uids, id.uid)
	}
	if len(uids) == 0 {
		return
	}
	var relays, blocked []string
	for _, uid := range uids {
		name, home := accountOf(uid)
		relays = append(relays, name)
		if uid != owner && !safeDirectoryAllows(safeDirectoryValuesFor(home), upstreamPath) {
			blocked = append(blocked, name)
		}
	}
	if len(blocked) == 0 {
		add(DoctorCheck{Repo: repo, Name: "Upstream access", Status: DoctorOK,
			Reason: "git lets " + strings.Join(relays, " and ") + " use " + upstreamPath + ": they own it or safe.directory allows it"})
		return
	}
	ownerName, _ := accountOf(owner)
	add(DoctorCheck{
		Repo:   repo,
		Name:   "Upstream access",
		Status: DoctorWarn,
		Reason: fmt.Sprintf("%s is owned by %s, and git refuses a repository another user owns, so relays run as %s will fail with \"detected dubious ownership\"", upstreamPath, ownerName, strings.Join(blocked, " and ")),
		Fix:    "git config --system --add safe.directory " + upstreamPath,
	})
}

func ownerUID(path string) (uint32, bool) {
	fi, err := os.Stat(path)
	if err != nil {
		return 0, false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return st.Uid, true
}

// accountOf names a uid and its home directory, where git reads that user's
// global config.
func accountOf(uid uint32) (name, home string) {
	u, err := user.LookupId(strconv.FormatUint(uint64(uid), 10))
	if err != nil {
		return fmt.Sprintf("uid %d", uid), ""
	}
	return u.Username, u.HomeDir
}

// safeDirectoryValuesFor is replaced in tests: the real one depends on this
// machine's git configuration.
var safeDirectoryValuesFor = safeDirectoryValues

// safeDirectoryValues lists safe.directory as a user with this home sees it:
// the system config plus their global config. It runs from / with a bare
// environment so neither doctor's working directory nor its environment
// leaks in.
func safeDirectoryValues(home string) []string {
	if home == "" {
		home = "/nonexistent"
	}
	cmd := exec.Command("git", "config", "--get-all", "safe.directory")
	cmd.Dir = "/"
	cmd.Env = []string{"PATH=" + os.Getenv("PATH"), "HOME=" + home}
	out, _ := cmd.Output()
	if len(out) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(out), "\n"), "\n")
}

// safeDirectoryAllows applies git's rules: "*" allows every repository, a
// value ending "/*" allows those under it, any other value must name the
// repository, and an empty value clears what came before.
func safeDirectoryAllows(values []string, path string) bool {
	path = filepath.Clean(path)
	allowed := false
	for _, v := range values {
		v = strings.TrimSpace(v)
		switch {
		case v == "":
			allowed = false
		case v == "*":
			allowed = true
		case strings.HasSuffix(v, "/*") && strings.HasPrefix(path+"/", strings.TrimSuffix(v, "*")):
			allowed = true
		case filepath.Clean(v) == path:
			allowed = true
		}
	}
	return allowed
}
