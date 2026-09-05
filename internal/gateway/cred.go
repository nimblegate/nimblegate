// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// CredentialStore holds the upstream push credential per repo. Never logged
// or surfaced to the dev.
type CredentialStore interface {
	Load(repo string) (string, error) // "" means no credential (e.g. file:// upstream)
	Save(repo, cred string) error
}

// FileCredentialStore stores the credential at <Root>/<repo>/credential (0640:
// the relay user reads it through the shared gateway group).
type FileCredentialStore struct{ Root string }

func (s FileCredentialStore) file(repo string) string {
	return filepath.Join(s.Root, repo, "credential")
}

func (s FileCredentialStore) Save(repo, cred string) error {
	if err := os.MkdirAll(filepath.Dir(s.file(repo)), 0o755); err != nil {
		return err
	}
	p := s.file(repo)
	if err := os.WriteFile(p, []byte(cred), 0o640); err != nil {
		return err
	}
	return os.Chmod(p, 0o640) // enforce the mode even if the file pre-existed with different perms
}

func (s FileCredentialStore) Load(repo string) (string, error) {
	b, err := os.ReadFile(s.file(repo))
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}
