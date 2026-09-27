// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"os"
	"path/filepath"
	"strings"

	"nimblegate/internal/engine"
)

var hugoConfigs = []string{
	"hugo.toml", "hugo.yaml", "hugo.yml", "hugo.json",
	"config.toml", "config.yaml", "config.yml", "config.json",
	"theme.toml",
}

var jekyllConfigs = []string{"_config.yml", "_config.yaml"}

// templateRoots maps a site generator's template folder to the config files
// that must sit beside it. The config is what makes a folder named layouts/
// a Hugo template root rather than a folder of real pages.
var templateRoots = map[string][]string{
	"layouts":   hugoConfigs,
	"_layouts":  jekyllConfigs,
	"_includes": jekyllConfigs,
}

var contentRoots = map[string][]string{
	"content": hugoConfigs,
}

// htmlTemplateFragment reports whether path is a site generator's template
// piece (a Hugo partial, a Jekyll include) rather than a whole page. The
// page-level web checks skip these; the built pages are checked instead.
func htmlTemplateFragment(ctx engine.CheckContext, path string) bool {
	return underSiteDir(ctx, path, templateRoots)
}

// siteContentFile reports whether path is a Hugo content file, where a link
// starting with "/" is a site address rather than a path in the repo.
func siteContentFile(ctx engine.CheckContext, path string) bool {
	return underSiteDir(ctx, path, contentRoots)
}

// underSiteDir walks up from path, stopping at the project root, and reports
// whether any ancestor folder is one of roots with a matching config file in
// its parent.
func underSiteDir(ctx engine.CheckContext, path string, roots map[string][]string) bool {
	abs := path
	if !filepath.IsAbs(abs) && ctx.ProjectRoot != "" {
		abs = filepath.Join(ctx.ProjectRoot, abs)
	}
	root := filepath.Clean(ctx.ProjectRoot)
	for dir := filepath.Dir(filepath.Clean(abs)); ; {
		if dir == root {
			return false
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		if configs, ok := roots[filepath.Base(dir)]; ok && anyFileIn(parent, configs) {
			return true
		}
		dir = parent
	}
}

func anyFileIn(dir string, names []string) bool {
	for _, n := range names {
		if info, err := os.Stat(filepath.Join(dir, n)); err == nil && !info.IsDir() {
			return true
		}
	}
	return false
}

var repoDocNames = map[string]bool{
	"README": true, "CHANGELOG": true, "CONTRIBUTING": true,
	"SECURITY": true, "CODE_OF_CONDUCT": true, "LICENSE": true,
}

// repoDocFile reports whether path is a repository document (README,
// CHANGELOG, ...) that a site build does not serve.
func repoDocFile(path string) bool {
	base := filepath.Base(path)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	return repoDocNames[strings.ToUpper(name)]
}
