// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package config

// AppConfig is the parsed ~/.appframes/config.toml (cross-project defaults).
type AppConfig struct {
	DefaultSeverity string          `toml:"default_severity"`
	Triggers        map[string]bool `toml:"triggers"`
}
