// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package gateway

import "testing"

func TestAccessStore_grantAllowsRevokeDenies(t *testing.T) {
	s := AccessStore{PolicyRoot: t.TempDir()}
	fp := "SHA256:abc"
	if ok, _ := s.Allows("demo", fp, false); ok {
		t.Fatal("ungranted key must be denied")
	}
	if err := s.Grant("demo", fp, "write", "alice@laptop"); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	if ok, _ := s.Allows("demo", fp, false); !ok {
		t.Error("write grant should allow fetch")
	}
	if ok, _ := s.Allows("demo", fp, true); !ok {
		t.Error("write grant should allow push")
	}
	if err := s.Revoke("demo", fp); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if ok, _ := s.Allows("demo", fp, false); ok {
		t.Error("revoked key must be denied")
	}
}

func TestAccessStore_readGrantCannotWrite(t *testing.T) {
	s := AccessStore{PolicyRoot: t.TempDir()}
	fp := "SHA256:def"
	if err := s.Grant("demo", fp, "read", "bob"); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	if ok, _ := s.Allows("demo", fp, false); !ok {
		t.Error("read grant should allow fetch")
	}
	if ok, _ := s.Allows("demo", fp, true); ok {
		t.Error("read grant must NOT allow push")
	}
}

func TestAccessStore_absentFileDeniesWithoutError(t *testing.T) {
	s := AccessStore{PolicyRoot: t.TempDir()}
	ok, err := s.Allows("ghost", "SHA256:x", false)
	if err != nil || ok {
		t.Errorf("absent ACL must deny without error; got ok=%v err=%v", ok, err)
	}
}

func TestAccessStore_grantIsIdempotentReplace(t *testing.T) {
	s := AccessStore{PolicyRoot: t.TempDir()}
	fp := "SHA256:ghi"
	_ = s.Grant("demo", fp, "read", "x")
	_ = s.Grant("demo", fp, "write", "x") // re-grant upgrades, not duplicates
	al, err := s.Load("demo")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(al.Grants) != 1 {
		t.Fatalf("re-grant should replace, got %d grants", len(al.Grants))
	}
	if ok, _ := s.Allows("demo", fp, true); !ok {
		t.Error("re-grant to write should allow push")
	}
}
