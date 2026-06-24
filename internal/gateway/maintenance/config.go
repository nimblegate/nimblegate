// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

// Package maintenance is the gateway's self-cleanup loop. It wakes on a
// configurable interval and runs `git gc --auto --quiet` per bare repo
// under --repos-root, so disk doesn't grow monotonically as pushes accumulate
// pack files. The gateway is a relay (not a backup), so this is the right
// place for the cleanup discipline - see .appframes/_design.md
// "Gateway maintenance loop" for the design rationale.
package maintenance

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/BurntSushi/toml"
)

const defaultAuditRetention = 30 * 24 * time.Hour
const defaultEventsRetention = 30 * 24 * time.Hour

// Config is the [maintenance] section of <policy-root>/gateway.toml.
type Config struct {
	Enabled              bool
	Interval             time.Duration
	DeadletterRetention  time.Duration
	AuditAcceptRetention time.Duration // accept records older than this are pruned
	AuditRejectRetention time.Duration // 0 = keep rejects/observed forever
	EventsRetention      time.Duration // _events.jsonl lines older than this are pruned
}

// DefaultConfig is what an operator gets when the file is missing or the
// [maintenance] section is absent: enabled, weekly, 30-day deadletter retention.
// Picked so that "install nimblegate and forget" produces correct long-term
// behavior.
func DefaultConfig() Config {
	return Config{
		Enabled:              true,
		Interval:             168 * time.Hour, // 1 week
		DeadletterRetention:  defaultDeadletterRetention,
		AuditAcceptRetention: defaultAuditRetention,
		AuditRejectRetention: 0,
		EventsRetention:      defaultEventsRetention,
	}
}

// minInterval guards against operator typos like interval = "5s" that would
// hammer the bare repos. git gc --auto is self-throttling but the loop itself
// shouldn't be tighter than a minute.
const minInterval = time.Minute

// Load reads <policy-root>/gateway.toml and returns the parsed [maintenance]
// config. Missing file or absent [maintenance] section → DefaultConfig, no
// error. Present but invalid → error.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return DefaultConfig(), nil
		}
		return Config{}, fmt.Errorf("maintenance: read %s: %w", path, err)
	}
	return parse(data)
}

// tomlShape mirrors only the keys we care about, so unknown sections elsewhere
// in <policy-root>/gateway.toml (added by future features) don't break this.
type tomlShape struct {
	Maintenance struct {
		Enabled    *bool   `toml:"enabled"`
		Interval   *string `toml:"interval"`
		Deadletter struct {
			Retention *string `toml:"retention"`
		} `toml:"deadletter"`
		Audit struct {
			AcceptRetention *string `toml:"accept_retention"`
			RejectRetention *string `toml:"reject_retention"`
		} `toml:"audit"`
		Events struct {
			Retention *string `toml:"retention"`
		} `toml:"events"`
	} `toml:"maintenance"`
}

func parse(data []byte) (Config, error) {
	var raw tomlShape
	if _, err := toml.Decode(string(data), &raw); err != nil {
		return Config{}, fmt.Errorf("maintenance: parse: %w", err)
	}
	cfg := DefaultConfig()
	if raw.Maintenance.Enabled != nil {
		cfg.Enabled = *raw.Maintenance.Enabled
	}
	if raw.Maintenance.Interval != nil {
		d, err := time.ParseDuration(*raw.Maintenance.Interval)
		if err != nil {
			return Config{}, fmt.Errorf("maintenance.interval: %w", err)
		}
		if d < minInterval {
			return Config{}, fmt.Errorf("maintenance.interval %s is below minimum %s", d, minInterval)
		}
		cfg.Interval = d
	}
	if raw.Maintenance.Deadletter.Retention != nil {
		d, err := time.ParseDuration(*raw.Maintenance.Deadletter.Retention)
		if err != nil {
			return Config{}, fmt.Errorf("maintenance.deadletter.retention: %w", err)
		}
		if d <= 0 {
			return Config{}, fmt.Errorf("maintenance.deadletter.retention must be positive; got %s", d)
		}
		cfg.DeadletterRetention = d
	}
	parseDur := func(key string, raw *string, allowZero bool) (time.Duration, bool, error) {
		if raw == nil {
			return 0, false, nil
		}
		d, err := time.ParseDuration(*raw)
		if err != nil {
			return 0, false, fmt.Errorf("maintenance.%s: %w", key, err)
		}
		if d < 0 || (d == 0 && !allowZero) {
			return 0, false, fmt.Errorf("maintenance.%s must be %s; got %s", key,
				map[bool]string{true: ">= 0", false: "> 0"}[allowZero], d)
		}
		return d, true, nil
	}
	if d, ok, err := parseDur("audit.accept_retention", raw.Maintenance.Audit.AcceptRetention, false); err != nil {
		return Config{}, err
	} else if ok {
		cfg.AuditAcceptRetention = d
	}
	if d, ok, err := parseDur("audit.reject_retention", raw.Maintenance.Audit.RejectRetention, true); err != nil {
		return Config{}, err
	} else if ok {
		cfg.AuditRejectRetention = d
	}
	if d, ok, err := parseDur("events.retention", raw.Maintenance.Events.Retention, false); err != nil {
		return Config{}, err
	} else if ok {
		cfg.EventsRetention = d
	}
	return cfg, nil
}
