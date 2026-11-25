// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"fmt"
	"os"
	"strings"
)

// credHelper answers git's credential request from the child's environment.
// It runs under sh -c with only variable names in its text, and printf and
// test are shell builtins, so the token appears in no process's argv.
const credHelper = `!f() { test "$1" = get && printf 'username=%s\npassword=%s\n' "$NBG_UPSTREAM_USER" "$NBG_UPSTREAM_PASS"; }; f`

// upstreamGitEnv returns the environment for a git command that talks to
// upstreamURL. The credential travels only in the environment, which other
// users cannot read (/proc/<pid>/environ is owner-only), never in an argv.
// The helper is scoped to upstreamURL, so a redirect to another host gets
// nothing, and credential.helper is reset first so no configured helper
// stores the token. extra are further git config pairs (e.g. safe.directory).
// A user:token credential splits at the first colon; a bare token goes as the
// username with an empty password, the same request the old token@ URL made.
func upstreamGitEnv(upstreamURL, cred string, extra ...[2]string) []string {
	env := os.Environ()
	cfg := extra
	if cred != "" && (strings.HasPrefix(upstreamURL, "https://") || strings.HasPrefix(upstreamURL, "http://")) {
		user, pass, _ := strings.Cut(cred, ":")
		cfg = append(cfg,
			[2]string{"credential.helper", ""},
			[2]string{"credential." + upstreamURL + ".helper", credHelper},
		)
		env = append(env, "NBG_UPSTREAM_USER="+user, "NBG_UPSTREAM_PASS="+pass)
	}
	if len(cfg) == 0 {
		return env
	}
	env = append(env, fmt.Sprintf("GIT_CONFIG_COUNT=%d", len(cfg)))
	for i, kv := range cfg {
		env = append(env, fmt.Sprintf("GIT_CONFIG_KEY_%d=%s", i, kv[0]), fmt.Sprintf("GIT_CONFIG_VALUE_%d=%s", i, kv[1]))
	}
	return env
}
