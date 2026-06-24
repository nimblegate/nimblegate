// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
)

// mixedContentSrcRegex matches src= / href= attribute values that
// reference http:// (not https://, not protocol-relative //, not data:,
// not mailto:, not tel:, not javascript:).
//
// Captures the full URL in group 1 so the reason can quote it.
var mixedContentSrcRegex = regexp.MustCompile(`(?i)(?:src|href)\s*=\s*["'](http://[^"']+)["']`)

// mixedContentExemptHosts are namespaces / URLs that don't count as
// mixed content because no real resource is fetched:
//
//   - Schemas, XML namespaces, RFC reference URLs (the URL is an
//     identifier, not a fetch target).
//   - Localhost / RFC1918 / dev hostnames in templates / examples.
//
// Anything matching one of these hosts is silently allowed.
var mixedContentExemptRegex = regexp.MustCompile(`(?i)^http://(` +
	`www\.w3\.org/.*|` + // XML namespaces (`xmlns` values commonly use http://)
	`schemas?\.[a-z0-9.-]+|` + // schemas.example.com / schema.org
	`localhost(:\d+)?|` +
	`127\.0\.0\.1(:\d+)?|` +
	`0\.0\.0\.0(:\d+)?|` +
	`192\.168\.\d+\.\d+(:\d+)?|` +
	`10\.\d+\.\d+\.\d+(:\d+)?|` +
	`172\.(?:1[6-9]|2[0-9]|3[01])\.\d+\.\d+(:\d+)?|` +
	`example\.(?:com|org|net)(:\d+)?|` +
	`tools\.ietf\.org/.*|` +
	`purl\.org/.*` +
	`)(/.*)?$`)

const mixedContentDisableMarker = "appframes:disable security/no-mixed-content-urls"
const mixedContentDisableLineMarker = "appframes:disable-next-line security/no-mixed-content-urls"
const mixedContentMaxFileBytes = 1 << 20 // 1 MiB

// NoMixedContentURLs scans shipping HTML / Svelte / Astro for `http://`
// URLs in src= / href= attributes. On HTTPS pages these are blocked by
// browsers as "mixed content" - images, scripts, stylesheets, and
// fetches over plain HTTP fail with no useful console message for non-
// devtools users.
//
// Exempts:
//   - XML namespace URLs (xmlns values)
//   - Schemas (schema.org, schemas.example.com, ...)
//   - Localhost / RFC1918 ranges
//   - example.com / example.org / example.net
//   - IETF / purl reference URLs
//
// Tier 2 BLOCK - the underlying breakage is silent in production but
// catastrophic for any resource that fails to load (broken images,
// missing scripts, failed analytics calls). Surface aggressively;
// suppress when intentional.
func NoMixedContentURLs(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "security/no-mixed-content-urls",
		Category: frames.CategorySecurity,
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
			if htmlApplicableFile(path) {
				files = append(files, path)
			}
			return nil
		})
	}

	var hits []string
	var hitsStruct []engine.Hit
	const hitCap = 20

filesLoop:
	for _, file := range files {
		if !htmlApplicableFile(file) {
			continue
		}
		if ShouldSkipPath(ctx, file) {
			continue
		}
		info, err := os.Stat(file)
		if err != nil || info.Size() > mixedContentMaxFileBytes {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		content := string(data)
		if strings.Contains(content, mixedContentDisableMarker) {
			continue
		}
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			if i > 0 && strings.Contains(lines[i-1], mixedContentDisableLineMarker) {
				continue
			}
			for _, m := range mixedContentSrcRegex.FindAllStringSubmatch(line, -1) {
				url := m[1]
				if mixedContentExemptRegex.MatchString(url) {
					continue
				}
				label := fmt.Sprintf("http:// URL in src/href attribute: %s (mixed-content; will fail on https pages)", url)
				hits = append(hits, fmt.Sprintf("%s:%d - %s", file, i+1, label))
				hitsStruct = append(hitsStruct, engine.Hit{File: file, Line: i + 1, Label: label})
				if len(hits) >= hitCap {
					break filesLoop
				}
			}
		}
	}

	if len(hits) == 0 {
		res.Outcome = engine.OutcomePass
		return res
	}
	res.Hits = hitsStruct
	res.Outcome = engine.OutcomeBlock
	res.Reason = "mixed-content URLs (http:// resources on https pages will be blocked): " + strings.Join(hits, "; ")
	res.Fix = "switch http:// to https:// for the referenced resource. If the resource is HTTPS-capable, the swap is mechanical. If the resource is HTTP-only, host a copy yourself or proxy through your own HTTPS endpoint. For development URLs (localhost / 127.0.0.1 / RFC1918 / example.com), the frame already exempts them - for other hosts that must stay HTTP, suppress per-line."
	return res
}
