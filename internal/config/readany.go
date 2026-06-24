// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"

	v2 "nimblegate/internal/config/v2"
)

// ReadResult carries the parsed config along with which schema version was
// detected. Exactly one of V1 / V2 is non-nil based on SchemaVersion.
type ReadResult struct {
	SchemaVersion int
	V1            *ProjectConfig // non-nil when SchemaVersion == 1
	V2            *v2.Config     // non-nil when SchemaVersion == 2
}

// schemaProbe is the minimum structure to detect [appframes.schema].version
// without committing to either schema's full parse. Used as the first pass in
// ReadAny.
type schemaProbe struct {
	Appframes struct {
		Schema struct {
			Version int `toml:"version"`
		} `toml:"schema"`
	} `toml:"appframes"`
}

// ReadAny reads the file at path and dispatches to the v1 or v2 parser based
// on the [appframes.schema].version field. Absence of that field implies v1
// (backwards-compat with all existing configs). Future schema versions return
// an error rather than silently picking the closest known version.
func ReadAny(path string) (*ReadResult, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}

	var probe schemaProbe
	if err := toml.Unmarshal(raw, &probe); err != nil {
		return nil, fmt.Errorf("config: probe schema in %s: %w", path, err)
	}

	switch probe.Appframes.Schema.Version {
	case 0, 1:
		// Absent or explicit v1 - load via existing v1 parser.
		v1cfg, err := LoadProject(path)
		if err != nil {
			return nil, fmt.Errorf("config: load v1 %s: %w", path, err)
		}
		return &ReadResult{SchemaVersion: 1, V1: &v1cfg}, nil

	case 2:
		v2cfg, err := v2.Load(path)
		if err != nil {
			return nil, fmt.Errorf("config: load v2 %s: %w", path, err)
		}
		return &ReadResult{SchemaVersion: 2, V2: v2cfg}, nil

	default:
		return nil, fmt.Errorf("config: unknown schema version %d in %s (this build supports v1 and v2)", probe.Appframes.Schema.Version, path)
	}
}
