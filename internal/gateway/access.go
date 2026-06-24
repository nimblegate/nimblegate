// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

const accessVersion = 1

// AccessGrant authorizes one SSH key (by fingerprint) on one repo.
type AccessGrant struct {
	Fingerprint string `json:"fingerprint"`       // SHA256:… form
	Access      string `json:"access"`            // "read" (fetch) or "write" (push+fetch)
	Comment     string `json:"comment,omitempty"` // human label, copied from the key
}

// AccessList is a repo's set of grants, stored at <policy-root>/<repo>/access.json.
type AccessList struct {
	Version int           `json:"version"`
	Grants  []AccessGrant `json:"grants"`
}

// AccessStore reads/writes per-repo ACLs. The forced-command shell (running as
// the git user) consults it to scope each key to specific repos; without a
// grant a key can reach nothing (deny by default) once scoped access is on.
type AccessStore struct{ PolicyRoot string }

func (s AccessStore) path(repo string) string {
	return filepath.Join(s.PolicyRoot, repo, "access.json")
}

// Load returns the repo's grants, or an empty list if the file is absent.
func (s AccessStore) Load(repo string) (AccessList, error) {
	b, err := os.ReadFile(s.path(repo))
	if errors.Is(err, fs.ErrNotExist) {
		return AccessList{Version: accessVersion}, nil
	}
	if err != nil {
		return AccessList{}, err
	}
	var al AccessList
	if err := json.Unmarshal(b, &al); err != nil {
		return AccessList{}, fmt.Errorf("parse access.json for %q: %w", repo, err)
	}
	return al, nil
}

func (s AccessStore) save(repo string, al AccessList) error {
	if err := os.MkdirAll(filepath.Dir(s.path(repo)), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(al, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path(repo), append(b, '\n'), 0o644)
}

// Grant authorizes fingerprint on repo at the given access level ("read" or
// "write"). Re-granting an existing fingerprint replaces it (idempotent upgrade/
// downgrade), never duplicates.
func (s AccessStore) Grant(repo, fingerprint, access, comment string) error {
	if access != "read" && access != "write" {
		return fmt.Errorf("access must be \"read\" or \"write\", got %q", access)
	}
	al, err := s.Load(repo)
	if err != nil {
		return err
	}
	g := AccessGrant{Fingerprint: fingerprint, Access: access, Comment: comment}
	replaced := false
	for i := range al.Grants {
		if al.Grants[i].Fingerprint == fingerprint {
			al.Grants[i] = g
			replaced = true
			break
		}
	}
	if !replaced {
		al.Grants = append(al.Grants, g)
	}
	al.Version = accessVersion
	return s.save(repo, al)
}

// Revoke removes a fingerprint's grant on repo (no-op if absent).
func (s AccessStore) Revoke(repo, fingerprint string) error {
	al, err := s.Load(repo)
	if err != nil {
		return err
	}
	out := al.Grants[:0]
	for _, g := range al.Grants {
		if g.Fingerprint != fingerprint {
			out = append(out, g)
		}
	}
	al.Grants = out
	al.Version = accessVersion
	return s.save(repo, al)
}

// Allows reports whether fingerprint may perform the op on repo. write=true
// requires a "write" grant; write=false (fetch) accepts "read" or "write". No
// grant → false (deny by default).
func (s AccessStore) Allows(repo, fingerprint string, write bool) (bool, error) {
	al, err := s.Load(repo)
	if err != nil {
		return false, err
	}
	for _, g := range al.Grants {
		if g.Fingerprint == fingerprint {
			if write {
				return g.Access == "write", nil
			}
			return g.Access == "read" || g.Access == "write", nil
		}
	}
	return false, nil
}
