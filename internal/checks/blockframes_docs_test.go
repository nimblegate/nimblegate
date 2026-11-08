// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"html"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/engine"
)

// blockFrames are BLOCK-severity frames a kit can switch on; a doc page that
// explains one of them must not be rejected by any of them.
var blockFrames = map[string]func(engine.CheckContext) engine.CheckResult{
	"security/no-private-keys-in-repo":             NoPrivateKeysInRepo,
	"security/no-bidi-override":                    NoBidiOverride,
	"security/no-invisible-tag-chars":              NoInvisibleTagChars,
	"security/no-zero-width-in-source":             NoZeroWidthInSource,
	"encoding/no-mixed-indent":                     NoMixedIndent,
	"encoding/consistent-line-endings":             ConsistentLineEndings,
	"encoding/no-en-dash-in-commands":              NoEnDashInCommands,
	"encoding/no-smart-quotes-in-config":           NoSmartQuotesInConfig,
	"encoding/no-bom":                              NoBOM,
	"encoding/yaml-no-tabs":                        YAMLNoTabs,
	"security/no-hardcoded-credentials":            NoHardcodedCredentials,
	"security/no-env-file-in-repo":                 NoEnvFileInRepo,
	"security/no-kubeconfig-in-repo":               NoKubeconfigInRepo,
	"security/no-terraform-state-in-repo":          NoTerraformStateInRepo,
	"commands/curl-pipe-shell":                     CurlPipeShell,
	"database/migration-script-explicit-env":       MigrationScriptExplicitEnv,
	"database/migration-verification-step":         MigrationVerificationStep,
	"database/sqlite-migration-idempotent-wrapper": SQLiteMigrationIdempotentWrapper,
}

// TestBlockFrames_OwnDocsPass: every frame doc, as markdown and as the
// HTML-escaped page the docs site renders, passes every BLOCK frame above.
func TestBlockFrames_OwnDocsPass(t *testing.T) {
	docs, err := filepath.Glob("../stdlib/frames/*/*.md")
	if err != nil || len(docs) == 0 {
		t.Fatalf("no frame docs found: %v", err)
	}
	for _, doc := range docs {
		raw, err := os.ReadFile(doc)
		if err != nil {
			t.Fatal(err)
		}
		name := filepath.Base(doc)
		// A doc's override section shows the disable marker as an example;
		// the rendered site wraps it in markup so it no longer applies, and
		// the page must pass on its own merits.
		inert := strings.ReplaceAll(string(raw), "appframes:disable", "appframes-disable")
		for _, f := range []struct{ rel, body string }{
			{"docs/" + name, inert},
			{"public/docs/" + name[:len(name)-3] + ".html", "<pre><code>" + html.EscapeString(inert) + "</code></pre>"},
		} {
			root := t.TempDir()
			writeSource(t, root, f.rel, f.body)
			ctx := engine.CheckContext{Trigger: engine.TriggerCLI, ProjectRoot: root, ExcludedDirs: DefaultExcludes()}
			for id, fn := range blockFrames {
				if res := fn(ctx); res.Outcome == engine.OutcomeBlock {
					t.Errorf("%s: %s = %s: %s", f.rel, id, res.Outcome, res.Reason)
				}
			}
		}
	}
}
