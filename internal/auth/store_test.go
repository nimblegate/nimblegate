// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package auth

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	tmp := t.TempDir()
	s, err := Open(filepath.Join(tmp, "_auth.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestStore_UserCount_initiallyZero(t *testing.T) {
	s := openTestStore(t)
	n, err := s.UserCount()
	if err != nil {
		t.Fatalf("UserCount: %v", err)
	}
	if n != 0 {
		t.Errorf("UserCount = %d, want 0", n)
	}
}

func TestStore_CreateUser_thenVerify(t *testing.T) {
	s := openTestStore(t)
	id, err := s.CreateUser("admin", "supersecret123")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if id <= 0 {
		t.Errorf("id = %d, want > 0", id)
	}
	gotID, err := s.VerifyPassword("admin", "supersecret123")
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if gotID != id {
		t.Errorf("VerifyPassword id = %d, want %d", gotID, id)
	}
}

func TestStore_CreateUser_rejectsDuplicate(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.CreateUser("admin", "supersecret123"); err != nil {
		t.Fatalf("first CreateUser: %v", err)
	}
	_, err := s.CreateUser("admin", "anotherpassword")
	if !errors.Is(err, ErrUserExists) {
		t.Errorf("second CreateUser err = %v, want ErrUserExists", err)
	}
}

func TestStore_CreateUser_rejectsShortInputs(t *testing.T) {
	s := openTestStore(t)
	cases := []struct{ name, user, pass string }{
		{"short-username", "a", "supersecret123"},
		{"short-password", "admin", "short"},
		{"empty-username", "", "supersecret123"},
		{"empty-password", "admin", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := s.CreateUser(c.user, c.pass); err == nil {
				t.Errorf("CreateUser(%q, …) expected error, got nil", c.user)
			}
		})
	}
}

func TestStore_VerifyPassword_wrongPassword(t *testing.T) {
	s := openTestStore(t)
	if _, err := s.CreateUser("admin", "supersecret123"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	_, err := s.VerifyPassword("admin", "wrongpassword")
	if !errors.Is(err, ErrBadPassword) {
		t.Errorf("err = %v, want ErrBadPassword", err)
	}
}

func TestStore_VerifyPassword_missingUser(t *testing.T) {
	s := openTestStore(t)
	_, err := s.VerifyPassword("nobody", "any")
	if !errors.Is(err, ErrUserNotFound) {
		t.Errorf("err = %v, want ErrUserNotFound", err)
	}
}

func TestStore_VerifyPassword_constantTimeOnMissing(t *testing.T) {
	// Heuristic: on a missing user, we still run a fake bcrypt to keep timing
	// in the same ballpark. This is a smell-test, not a hard guarantee - just
	// confirm both calls take a similar order of magnitude (within 10x), and
	// that "missing" doesn't return instantly (which would leak the existence
	// of the user via timing).
	s := openTestStore(t)
	if _, err := s.CreateUser("admin", "supersecret123"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	t0 := time.Now()
	_, _ = s.VerifyPassword("admin", "wrongpassword")
	dExists := time.Since(t0)
	t0 = time.Now()
	_, _ = s.VerifyPassword("nobody", "wrongpassword")
	dMissing := time.Since(t0)
	if dMissing < dExists/10 {
		t.Errorf("missing-user verify (%v) << existing-user verify (%v) - timing leak", dMissing, dExists)
	}
}

func TestStore_Sessions_createLookupDelete(t *testing.T) {
	s := openTestStore(t)
	uid, err := s.CreateUser("admin", "supersecret123")
	if err != nil {
		t.Fatal(err)
	}
	sid, err := s.NewSession(uid, time.Hour)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if len(sid) != 64 {
		t.Errorf("session id len = %d, want 64 hex chars", len(sid))
	}
	got, err := s.LookupSession(sid)
	if err != nil {
		t.Fatalf("LookupSession: %v", err)
	}
	if got != uid {
		t.Errorf("LookupSession uid = %d, want %d", got, uid)
	}
	if err := s.DeleteSession(sid); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}
	if _, err := s.LookupSession(sid); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("after delete err = %v, want ErrSessionNotFound", err)
	}
}

func TestStore_Sessions_expiredNotReturned(t *testing.T) {
	s := openTestStore(t)
	uid, _ := s.CreateUser("admin", "supersecret123")
	sid, err := s.NewSession(uid, -1*time.Second) // already expired
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if _, err := s.LookupSession(sid); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("expired session err = %v, want ErrSessionNotFound", err)
	}
}

func TestStore_SweepExpiredSessions(t *testing.T) {
	s := openTestStore(t)
	uid, _ := s.CreateUser("admin", "supersecret123")
	// One expired, one fresh.
	expSid, _ := s.NewSession(uid, -1*time.Second)
	freshSid, _ := s.NewSession(uid, time.Hour)
	if err := s.SweepExpiredSessions(); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	// Fresh still lookable, expired now gone.
	if _, err := s.LookupSession(freshSid); err != nil {
		t.Errorf("fresh session lost: %v", err)
	}
	if _, err := s.LookupSession(expSid); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("expired session still present after Sweep")
	}
}

// --- setup token ---

func TestSetupToken_generatesValidFormat(t *testing.T) {
	tmp := t.TempDir()
	tok, fresh, err := EnsureSetupToken(tmp)
	if err != nil {
		t.Fatalf("EnsureSetupToken: %v", err)
	}
	if !fresh {
		t.Errorf("fresh = false, want true on first call")
	}
	// XXXX-XXXX-XXXX-XXXX = 19 chars
	if len(tok) != 19 {
		t.Errorf("token len = %d, want 19; got %q", len(tok), tok)
	}
	// File exists with mode 0600.
	st, err := os.Stat(SetupTokenPath(tmp))
	if err != nil {
		t.Fatalf("Stat token: %v", err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("token file perm = %v, want 0600", st.Mode().Perm())
	}
}

func TestSetupToken_idempotent(t *testing.T) {
	tmp := t.TempDir()
	first, _, _ := EnsureSetupToken(tmp)
	second, fresh, err := EnsureSetupToken(tmp)
	if err != nil {
		t.Fatalf("second EnsureSetupToken: %v", err)
	}
	if fresh {
		t.Errorf("second call fresh = true, want false")
	}
	if first != second {
		t.Errorf("token changed between calls: %q vs %q", first, second)
	}
}

func TestSetupToken_consume_correct(t *testing.T) {
	tmp := t.TempDir()
	tok, _, _ := EnsureSetupToken(tmp)
	ok, err := ConsumeSetupToken(tmp, tok)
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if !ok {
		t.Errorf("Consume(correct) = false, want true")
	}
	// File should be gone now.
	if _, err := os.Stat(SetupTokenPath(tmp)); !os.IsNotExist(err) {
		t.Errorf("token file still present after consume: err=%v", err)
	}
}

func TestSetupToken_consume_wrong(t *testing.T) {
	tmp := t.TempDir()
	_, _, _ = EnsureSetupToken(tmp)
	ok, err := ConsumeSetupToken(tmp, "WRONG-WRONG-WRONG-WRONG")
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if ok {
		t.Errorf("Consume(wrong) = true, want false")
	}
	// File should STILL be present.
	if _, err := os.Stat(SetupTokenPath(tmp)); err != nil {
		t.Errorf("token file deleted on wrong attempt: %v", err)
	}
}

func TestSetupToken_consume_acceptsBothFormats(t *testing.T) {
	tmp := t.TempDir()
	tok, _, _ := EnsureSetupToken(tmp)
	// Strip dashes; lowercase. Should still consume.
	stripped := strings.ToLower(strings.ReplaceAll(tok, "-", ""))
	ok, err := ConsumeSetupToken(tmp, stripped)
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if !ok {
		t.Errorf("Consume(stripped) = false, want true")
	}
}

func TestSetupToken_consume_noFile(t *testing.T) {
	tmp := t.TempDir()
	ok, err := ConsumeSetupToken(tmp, "ABCD-EFGH-1234-5678")
	if err != nil {
		t.Fatalf("Consume on absent file: %v", err)
	}
	if ok {
		t.Errorf("Consume on absent file = true, want false")
	}
}

func TestAPITokenLifecycle(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, err := s.CreateAPIToken("  "); err == nil {
		t.Error("blank label must error")
	}
	tok, err := s.CreateAPIToken("ci-agent")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(tok, "nbg_") || len(tok) != 4+64 {
		t.Errorf("token shape wrong: %q", tok)
	}
	ok, err := s.VerifyAPIToken(tok)
	if err != nil || !ok {
		t.Fatalf("fresh token must verify: %v %v", ok, err)
	}
	if ok, _ := s.VerifyAPIToken("nbg_" + strings.Repeat("0", 64)); ok {
		t.Error("unknown token must not verify")
	}

	tok2, err := s.CreateAPIToken("second")
	if err != nil {
		t.Fatal(err)
	}
	list, err := s.ListAPITokens()
	if err != nil || len(list) != 2 || list[0].Label != "second" || list[1].Label != "ci-agent" {
		t.Fatalf("list wrong (newest first): %+v %v", list, err)
	}
	if list[1].ID <= 0 || list[1].CreatedAt <= 0 || list[1].RevokedAt != 0 {
		t.Fatalf("list fields wrong: %+v", list[1])
	}
	if err := s.RevokeAPIToken(list[1].ID); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.VerifyAPIToken(tok); ok {
		t.Error("revoked token must not verify")
	}
	if ok, _ := s.VerifyAPIToken(tok2); !ok {
		t.Error("revoking one token must not affect another")
	}
	list2, err := s.ListAPITokens()
	if err != nil || len(list2) != 2 {
		t.Fatalf("list2 wrong: %+v %v", list2, err)
	}
	if list2[1].RevokedAt == 0 {
		t.Error("RevokedAt must be set after revoke")
	}
	if list2[0].RevokedAt != 0 {
		t.Error("untouched token must stay active")
	}
}
