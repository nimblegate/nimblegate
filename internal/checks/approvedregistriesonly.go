// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"nimblegate/internal/canonical"
	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
)

var (
	arPomRepoOpen  = regexp.MustCompile(`<(repository|pluginRepository)\b`)
	arPomRepoClose = regexp.MustCompile(`</(repository|pluginRepository)>`)
	arPomURL       = regexp.MustCompile(`<url>\s*(https?://[^<\s]+)\s*</url>`)
	arGradleCall   = regexp.MustCompile(`\b(mavenCentral|jcenter|gradlePluginPortal|google)\s*\(\s*\)`)
	arGradleURL    = regexp.MustCompile(`\b(?:url\b[^"'\n]*|uri\(\s*)["'](https?://[^"']+)["']`)
	arNpmrcLine    = regexp.MustCompile(`^\s*(?:@[^:=\s]+:)?registry\s*=\s*(https?://\S+)`)
	arYarnLine     = regexp.MustCompile(`\bnpmRegistryServer:\s*["']?(https?://[^"'\s]+)`)
	arPipFlag      = regexp.MustCompile(`(?:--index-url|--extra-index-url|(?:^|\s)-i)\s+(https?://\S+)`)
	arPipConf      = regexp.MustCompile(`^\s*(?:extra-)?index-url\s*=\s*(https?://\S+)`)
)

const arDisableMarker = "appframes:disable commands/approved-registries-only"
const arDisableLineMarker = "appframes:disable-next-line commands/approved-registries-only"

// approvedRegistriesApplicableFile matches the frame's applies-to set.
func approvedRegistriesApplicableFile(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	switch base {
	case "pom.xml", ".npmrc", ".yarnrc.yml", "pip.conf", "pip.ini",
		"build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts":
		return true
	}
	return strings.HasPrefix(base, "requirements") && strings.HasSuffix(base, ".txt")
}

// arPrivateHost reports whether a host is clearly not a public registry:
// loopback, or a bare intranet hostname without a dot (http://nexus:8081).
func arPrivateHost(host string) bool {
	h := strings.ToLower(host)
	if h == "localhost" || h == "127.0.0.1" || h == "::1" || h == "0.0.0.0" {
		return true
	}
	return !strings.Contains(h, ".")
}

// arAllowlist is the parsed .appframes/_canonical/approved-registries.toml.
// entries hold lowercased hosts, host:port forms, and gradle shortcut names.
type arAllowlist struct {
	present bool
	entries map[string]bool
}

func (a arAllowlist) allowsHost(host, port string) bool {
	if !a.present {
		return false
	}
	h := strings.ToLower(host)
	if a.entries[h] {
		return true
	}
	return port != "" && a.entries[h+":"+port]
}

func loadRegistryAllowlist(root string) (arAllowlist, error) {
	path := filepath.Join(root, ".appframes", "_canonical", "approved-registries.toml")
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return arAllowlist{}, nil
	}
	tbl, err := canonical.Load(path)
	if err != nil {
		return arAllowlist{}, err
	}
	out := arAllowlist{present: true, entries: map[string]bool{}}
	if section, ok := tbl.Section("registries"); ok {
		for _, v := range section {
			out.entries[strings.ToLower(strings.TrimSpace(v))] = true
		}
	}
	return out, nil
}

// arCollectURL resolves a matched registry URL against the allowlist and
// returns the host to report, or "" when the source is acceptable.
func arCollectURL(raw string, allow arAllowlist) string {
	u, err := url.Parse(strings.TrimRight(raw, `"',;)`))
	if err != nil || u.Hostname() == "" {
		return ""
	}
	host := u.Hostname()
	if arPrivateHost(host) {
		return ""
	}
	if allow.allowsHost(host, u.Port()) {
		return ""
	}
	return host
}

// ApprovedRegistriesOnly flags dependency-source declarations (Maven/Gradle
// repositories, npm/yarn registries, pip index URLs) whose host is not on the
// project's approved-registries allowlist. With no allowlist configured,
// every declared public registry fires as WARN inventory.
//
// Scope contract (file-scan scope):
//   - cli + empty ChangedFiles → project-wide walk over applicable files
//   - pre-commit + empty ChangedFiles → PASS (matches real hook)
//   - non-empty ChangedFiles → scan only those (still filtered by name)
//   - noise-dir exclusion uniform via ShouldSkipPath
func ApprovedRegistriesOnly(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "commands/approved-registries-only",
		Category: frames.CategoryCommands,
	}

	allow, err := loadRegistryAllowlist(ctx.ProjectRoot)
	if err != nil {
		res.Outcome = engine.OutcomeError
		res.Reason = "approved-registries.toml: " + err.Error()
		return res
	}

	files := ctx.ChangedFiles
	if len(files) == 0 && ctx.Trigger == engine.TriggerCLI {
		_ = filepath.WalkDir(ctx.ProjectRoot, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if ShouldSkipPath(ctx, path) {
					return filepath.SkipDir
				}
				return nil
			}
			if approvedRegistriesApplicableFile(path) {
				files = append(files, path)
			}
			return nil
		})
	}

	var hits []string
	var hitsStruct []engine.Hit
	const hitCap = 10

filesLoop:
	for _, file := range files {
		if !approvedRegistriesApplicableFile(file) {
			continue
		}
		if ShouldSkipPath(ctx, file) {
			continue
		}
		data, ok := ReadFileBounded(file, DefaultMaxFileBytes)
		if !ok {
			continue
		}
		content := string(data)
		if strings.Contains(content, arDisableMarker) {
			continue
		}

		base := strings.ToLower(filepath.Base(file))
		isPom := base == "pom.xml"
		isGradle := strings.HasPrefix(base, "build.gradle") || strings.HasPrefix(base, "settings.gradle")
		isNpmrc := base == ".npmrc"
		isYarn := base == ".yarnrc.yml"
		isPipConf := base == "pip.conf" || base == "pip.ini"
		isReq := strings.HasPrefix(base, "requirements")

		inPomRepo := false
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			if i > 0 && strings.Contains(lines[i-1], arDisableLineMarker) {
				continue
			}

			var host string
			switch {
			case isPom:
				if arPomRepoOpen.MatchString(line) {
					inPomRepo = true
				}
				if inPomRepo {
					if m := arPomURL.FindStringSubmatch(line); m != nil {
						host = arCollectURL(m[1], allow)
					}
				}
				if arPomRepoClose.MatchString(line) {
					inPomRepo = false
				}
			case isGradle:
				if m := arGradleCall.FindStringSubmatch(line); m != nil {
					if !allow.entries[strings.ToLower(m[1])] {
						host = m[1] + "()"
					}
				} else if m := arGradleURL.FindStringSubmatch(line); m != nil {
					host = arCollectURL(m[1], allow)
				}
			case isNpmrc:
				if m := arNpmrcLine.FindStringSubmatch(line); m != nil {
					host = arCollectURL(m[1], allow)
				}
			case isYarn:
				if m := arYarnLine.FindStringSubmatch(line); m != nil {
					host = arCollectURL(m[1], allow)
				}
			case isPipConf:
				if m := arPipConf.FindStringSubmatch(line); m != nil {
					host = arCollectURL(m[1], allow)
				}
			case isReq:
				if m := arPipFlag.FindStringSubmatch(line); m != nil {
					host = arCollectURL(m[1], allow)
				}
			}

			if host == "" {
				continue
			}
			hits = append(hits, fmt.Sprintf("%s:%d - %s", file, i+1, host))
			hitsStruct = append(hitsStruct, engine.Hit{File: file, Line: i + 1, Label: host})
			if len(hits) >= hitCap {
				break filesLoop
			}
		}
	}

	if len(hits) == 0 {
		res.Outcome = engine.OutcomePass
		return res
	}
	res.Hits = hitsStruct
	res.Outcome = engine.OutcomeWarn
	res.Reason = "dependency sources outside the approved registries: " + strings.Join(hits, "; ")
	res.Fix = "route dependencies through your internal registry mirror, add the host to .appframes/_canonical/approved-registries.toml, or add `# appframes:disable-next-line commands/approved-registries-only` above an intentional exception"
	return res
}
