// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// License is the operator's self-reported commercial-license attestation,
// stored in <policy-root>/license.toml. It is honor-system only: never
// validated, never sent anywhere. Default value (Commercial=false) means the
// install is treated as non-commercial.
type License struct {
	Commercial bool   `toml:"commercial"`
	OrderRef   string `toml:"order-ref"`
}

// licenseFile is the dashboard-owned attestation file. Separate from the
// operator-hand-edited gateway.toml so a dashboard write never clobbers
// operator comments or the [maintenance] block.
type licenseFile struct {
	License License `toml:"license"`
}

func licensePath(policyRoot string) string {
	return filepath.Join(policyRoot, "license.toml")
}

// LoadLicense reads <policy-root>/license.toml. A missing file returns the
// default (non-commercial), no error; a present-but-malformed file returns an
// error.
func LoadLicense(policyRoot string) (License, error) {
	data, err := os.ReadFile(licensePath(policyRoot))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return License{}, nil
		}
		return License{}, err
	}
	var lf licenseFile
	if err := toml.Unmarshal(data, &lf); err != nil {
		return License{}, err
	}
	return lf.License, nil
}

// SaveLicense writes the attestation to <policy-root>/license.toml, creating
// the policy root if it does not exist. The file is rewritten wholesale.
func SaveLicense(policyRoot string, lic License) error {
	if err := os.MkdirAll(policyRoot, 0o755); err != nil {
		return err
	}
	f, err := os.Create(licensePath(policyRoot))
	if err != nil {
		return err
	}
	defer f.Close()
	return toml.NewEncoder(f).Encode(licenseFile{License: lic})
}
