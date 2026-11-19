// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"bytes"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
)

const clientEnvDisableMarker = "appframes:disable security/no-secret-in-client-env"
const clientEnvDisableLineMarker = "appframes:disable-next-line security/no-secret-in-client-env"

// clientEnvName matches a variable whose prefix tells the framework to inline
// its value into the browser bundle. The leading \b keeps PUBLIC_ from
// matching inside NEXT_PUBLIC_ and friends, which carry their own entry.
const clientEnvName = `\b((?:VITE|NEXT_PUBLIC|REACT_APP|EXPO_PUBLIC|NUXT_PUBLIC|PUBLIC)_[A-Z0-9_]+)\b`

// clientEnvRefRe finds the name where code reads it: import.meta.env.X,
// process.env.X, env.X, env["X"].
var clientEnvRefRe = regexp.MustCompile(`(?:import\.meta\.env|process\.env|\benv)\s*(?:\.\s*|\[\s*['"])` + clientEnvName)

// clientEnvDefRe finds the name where it is defined: KEY=value in env files
// and Dockerfiles (ENV/ARG), KEY: value in YAML/TOML/JSON and framework config.
var clientEnvDefRe = regexp.MustCompile(`^\s*(?:-\s*)?(?:export\s+|ENV\s+|ARG\s+)?["']?` + clientEnvName + `["']?\s*[:=]`)

// clientEnvDockerRe finds Dockerfile build args and env without a value
// (ARG VITE_X), which the build reads from --build-arg.
var clientEnvDockerRe = regexp.MustCompile(`^\s*(?:ARG|ENV)\s+` + clientEnvName)

// clientEnvBareRe finds bare names, used only in files importing SvelteKit's
// public env modules, where PUBLIC_ variables are used without a prefix.
var clientEnvBareRe = regexp.MustCompile(clientEnvName)

// clientEnvServerMarkers make a client-prefixed name unambiguously server-only.
var clientEnvServerMarkers = []string{"SECRET", "PRIVATE", "SERVICE_ROLE", "PASSWORD", "PASSWD", "DATABASE_URL"}

// clientEnvProviders are services whose API keys are server-only by design;
// they count only with KEY or TOKEN in the name (VITE_OPENAI_MODEL is fine).
var clientEnvProviders = []string{"OPENAI", "ANTHROPIC", "RESEND", "SENDGRID", "TWILIO_AUTH", "MAILGUN", "POSTMARK"}

// clientEnvPublicMarkers are values meant for the browser; they never fire.
var clientEnvPublicMarkers = []string{"PUBLISHABLE", "ANON", "PUBLIC_KEY", "SITE_KEY", "MEASUREMENT_ID"}

// clientEnvSecretName reports whether a client-prefixed name holds a
// server-only secret.
func clientEnvSecretName(name string) bool {
	rest := name
	for _, p := range []string{"NEXT_PUBLIC_", "EXPO_PUBLIC_", "NUXT_PUBLIC_", "REACT_APP_", "VITE_", "PUBLIC_"} {
		if strings.HasPrefix(name, p) {
			rest = name[len(p):]
			break
		}
	}
	for _, m := range clientEnvPublicMarkers {
		if strings.Contains(rest, m) {
			return false
		}
	}
	for _, m := range clientEnvServerMarkers {
		if strings.Contains(rest, m) {
			return true
		}
	}
	for _, p := range clientEnvProviders {
		if strings.Contains(rest, p) && (strings.Contains(rest, "KEY") || strings.Contains(rest, "TOKEN")) {
			return true
		}
	}
	return false
}

// clientEnvDocExt are prose formats: tutorials there name variables to warn
// about them, nothing is built from them.
var clientEnvDocExt = map[string]bool{".md": true, ".mdx": true, ".rst": true, ".txt": true, ".adoc": true, ".html": true, ".htm": true}

// clientEnvScriptExt are files that can import SvelteKit's env modules.
var clientEnvScriptExt = map[string]bool{".svelte": true, ".js": true, ".ts": true, ".mjs": true, ".mts": true, ".cjs": true, ".jsx": true, ".tsx": true}

// clientEnvDefFile reports files where a line defines variables for the
// build: env files, Dockerfiles, compose/CI YAML, TOML, JSON, framework config.
func clientEnvDefFile(rel string) bool {
	base := filepath.Base(rel)
	switch {
	case strings.HasPrefix(base, ".env"), strings.HasPrefix(base, "Dockerfile"),
		strings.Contains(base, ".config."), strings.HasPrefix(base, "Containerfile"):
		return true
	}
	switch filepath.Ext(base) {
	case ".yml", ".yaml", ".toml", ".json", ".jsonc":
		return true
	}
	return false
}

// NoSecretInClientEnv blocks server-only secrets given a variable name that
// the framework inlines into the browser bundle (VITE_, NEXT_PUBLIC_,
// PUBLIC_, REACT_APP_, EXPO_PUBLIC_, NUXT_PUBLIC_). The key itself usually
// never reaches git (.env is ignored), but the name does, in code that reads
// it or config that defines it, and the build ships the value to every
// visitor. Only unambiguous names fire: SECRET, PRIVATE, SERVICE_ROLE,
// PASSWORD, DATABASE_URL, or a server-only provider's KEY/TOKEN. Names marked
// public (PUBLISHABLE, ANON, PUBLIC_KEY, SITE_KEY, MEASUREMENT_ID) never fire.
//
// Redaction guarantee: only the variable name is reported, never a value.
func NoSecretInClientEnv(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "security/no-secret-in-client-env",
		Category: frames.CategorySecurity,
	}
	var hits []string
	var hitsStruct []engine.Hit
	const hitCap = 10

filesLoop:
	for _, file := range scanFileList(ctx) {
		if ShouldSkipPath(ctx, file) || isMinifiedPath(file) {
			continue
		}
		rel := relPath(ctx, file)
		if isThirdPartyPath(rel) || isTestOrFixturePath(rel) || clientEnvDocExt[strings.ToLower(filepath.Ext(rel))] {
			continue
		}
		data, ok := ReadFileBounded(file, DefaultMaxFileBytes)
		if !ok || bytes.IndexByte(data, 0) >= 0 {
			continue
		}
		content := string(data)
		if fileDisabledByMarker(content, clientEnvDisableMarker) {
			continue
		}
		defFile := clientEnvDefFile(rel)
		svelteEnv := clientEnvScriptExt[strings.ToLower(filepath.Ext(rel))] &&
			(strings.Contains(content, "$env/static/public") || strings.Contains(content, "$env/dynamic/public"))
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			if i > 0 && lineCarriesMarker(lines[i-1], clientEnvDisableLineMarker) {
				continue
			}
			var names []string
			for _, m := range clientEnvRefRe.FindAllStringSubmatch(line, -1) {
				names = append(names, m[1])
			}
			if defFile {
				if m := clientEnvDefRe.FindStringSubmatch(line); m != nil {
					names = append(names, m[1])
				} else if m := clientEnvDockerRe.FindStringSubmatch(line); m != nil {
					names = append(names, m[1])
				}
			}
			if svelteEnv {
				for _, m := range clientEnvBareRe.FindAllStringSubmatch(line, -1) {
					if strings.HasPrefix(m[1], "PUBLIC_") {
						names = append(names, m[1])
					}
				}
			}
			for _, n := range names {
				if !clientEnvSecretName(n) {
					continue
				}
				label := n + " is shipped to the browser"
				hits = append(hits, fmt.Sprintf("%s:%d - %s", file, i+1, label))
				hitsStruct = append(hitsStruct, engine.Hit{File: file, Line: i + 1, Label: label})
				if len(hits) >= hitCap {
					break filesLoop
				}
				break
			}
		}
	}

	if len(hits) == 0 {
		res.Outcome = engine.OutcomePass
		return res
	}
	res.Hits = hitsStruct
	res.Outcome = engine.OutcomeBlock
	res.Reason = "server-only secret behind a browser-exposed env prefix (the build inlines its value into the frontend): " + strings.Join(hits, "; ")
	res.Fix = "rename the variable without the client prefix (VITE_, NEXT_PUBLIC_, PUBLIC_, REACT_APP_, EXPO_PUBLIC_, NUXT_PUBLIC_) and use it only in server code (an API route, server function or edge function); if a build with the old name was ever deployed, ROTATE the key; use `# appframes:disable-next-line security/no-secret-in-client-env` only for a value that is truly public"
	return res
}
