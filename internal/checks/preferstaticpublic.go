// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"nimblegate/internal/engine"
	"nimblegate/internal/frames"
)

const preferStaticDisableMarker = "appframes:disable app-correctness/prefer-static-public"
const preferStaticDisableLineMarker = "appframes:disable-next-line app-correctness/prefer-static-public"
const preferStaticMaxFileBytes = 1 << 20 // 1 MiB

// PreferStaticPublic surfaces an INFO-level finding on any
// `$env/dynamic/public` import. The dynamic module reads env at runtime
// and is genuinely useful only when the value must change without a
// redeploy. For most public flags, `$env/static/public` is safer:
// inlined at build time, undefined imports return undefined cleanly,
// no runtime crash on missing env.
//
// This frame is the companion to `app-correctness/dynamic-env-declared`.
// dynamic-env-declared catches THE BUG (undeclared var crashes prod);
// prefer-static-public flags THE PATTERN as an opportunity to switch.
// They can both fire on the same line - different severities, different
// information.
//
// Severity is INFO so it surfaces in `nimblegate status` and `nimblegate
// audit analyze` without blocking commits - the user makes the call.
func PreferStaticPublic(ctx engine.CheckContext) engine.CheckResult {
	res := engine.CheckResult{
		FrameID:  "app-correctness/prefer-static-public",
		Category: frames.CategoryAppCorrectness,
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
			if dynamicEnvApplicableFile(path) {
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
		if !dynamicEnvApplicableFile(file) {
			continue
		}
		if ShouldSkipPath(ctx, file) {
			continue
		}
		info, err := os.Stat(file)
		if err != nil || info.Size() > preferStaticMaxFileBytes {
			continue
		}
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		content := string(data)
		if strings.Contains(content, preferStaticDisableMarker) {
			continue
		}
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			if i > 0 && strings.Contains(lines[i-1], preferStaticDisableLineMarker) {
				continue
			}
			if !dynamicEnvImportRegex.MatchString(line) {
				continue
			}
			label := "imports from $env/dynamic/public; consider $env/static/public (inlined at build time)"
			hits = append(hits, fmt.Sprintf("%s:%d - %s", file, i+1, label))
			hitsStruct = append(hitsStruct, engine.Hit{File: file, Line: i + 1, Label: label})
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
	res.Outcome = engine.OutcomeInfo
	res.Reason = "$env/dynamic/public usage (prefer static for build-time vars): " + strings.Join(hits, "; ")
	res.Fix = "if the value is known at build time, switch to `$env/static/public`: inlined at build, undefined imports return undefined cleanly, no runtime crash on missing env. Only keep dynamic when you genuinely need to change the value without a redeploy."
	return res
}
