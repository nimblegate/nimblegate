// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"nimblegate/internal/checks"
	"nimblegate/internal/engine"
)

// BuiltinCheckFuncs returns the map of stdlib frame ID -> check function.
func BuiltinCheckFuncs() map[string]engine.CheckFunc {
	return map[string]engine.CheckFunc{
		"git/folder-branch-lock":                       checks.FolderBranchLock,
		"git/no-amend-pushed-commits":                  checks.NoAmendPushedCommits,
		"git/no-bypass-pre-commit":                     checks.NoBypassPreCommit,
		"git/no-force-push-main":                       checks.NoForcePushMain,
		"git/no-lfsconfig-changes":                     checks.NoLFSConfigChanges,
		"commands/apt-purge-preview":                   checks.AptPurgePreview,
		"commands/curl-pipe-shell":                     checks.CurlPipeShell,
		"database/migration-script-explicit-env":       checks.MigrationScriptExplicitEnv,
		"database/migration-verification-step":         checks.MigrationVerificationStep,
		"database/sqlite-migration-idempotent-wrapper": checks.SQLiteMigrationIdempotentWrapper,
		"filesystem/rm-rf-protected-paths":             checks.RmRfProtectedPaths,
		"network/cidr-host-bits-zero":                  checks.CIDRHostBitsZero,
		"network/no-localhost-in-proxy-config":         checks.NoLocalhostInProxyConfig,
		"security/no-innerHTML-user-input":             checks.NoInnerHTMLUserInput,
		"security/no-hardcoded-credentials":            checks.NoHardcodedCredentials,
		"security/no-private-keys-in-repo":             checks.NoPrivateKeysInRepo,
		"security/no-mixed-content-urls":               checks.NoMixedContentURLs,
		"security/cf-pages-headers-baseline":           checks.CFPagesHeadersBaseline,
		"security/no-bidi-override":                    checks.NoBidiOverride,
		"security/no-invisible-tag-chars":              checks.NoInvisibleTagChars,
		"security/no-zero-width-in-source":             checks.NoZeroWidthInSource,
		"security/no-homoglyph-identifiers":            checks.NoHomoglyphIdentifiers,
		"encoding/no-bom":                              checks.NoBOM,
		"encoding/no-smart-quotes-in-config":           checks.NoSmartQuotesInConfig,
		"encoding/yaml-no-tabs":                        checks.YAMLNoTabs,
		"encoding/consistent-line-endings":             checks.ConsistentLineEndings,
		"encoding/no-mixed-indent":                     checks.NoMixedIndent,
		"encoding/no-en-dash-in-commands":              checks.NoEnDashInCommands,
		"encoding/no-non-printable":                    checks.NoNonPrintable,
		"encoding/no-zero-width-in-content":            checks.NoZeroWidthInContent,
		"app-correctness/cf-graphql-dataset-by-window": checks.CFGraphQLDatasetByWindow,
		"app-correctness/cf-graphql-schema-match":      checks.CFGraphQLSchemaMatch,
		"app-correctness/dynamic-env-declared":         checks.DynamicEnvDeclared,
		"app-correctness/prefer-static-public":         checks.PreferStaticPublic,
		"database/schema-vs-code-drift":                checks.SchemaVsCodeDrift,
		"app-correctness/top-of-page-import-safety":    checks.TopOfPageImportSafety,
		"documentation/cross-branch-id-consistency":    checks.CrossBranchIDConsistency,
		"documentation/dated-todo":                     checks.DatedTodo,
		"documentation/doc-touches-with-code":          checks.DocTouchesWithCode,
		"documentation/markdown-link-check-internal":   checks.MarkdownLinkCheckInternal,
		"web/html-required-meta":                       checks.HTMLRequiredMeta,
		"web/html-seo-meta":                            checks.HTMLSEOMeta,
		"web/html-img-alt":                             checks.HTMLImgAlt,
		"web/html-markup-valid":                        checks.HTMLMarkupValid,
		"web/html-placeholder-content":                 checks.HTMLPlaceholderContent,
	}
}
