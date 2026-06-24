// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

// Package auth backs the v0.1.0 single-admin dashboard authentication:
// bcrypt-hashed user credentials + server-side sessions, both in SQLite via
// modernc.org/sqlite (pure-Go). No JWT - for a single-instance free-tier,
// server-side sessions are simpler and trivially revocable.
//
// Scope is deliberately small: one admin user, no roles, no email reset, no
// MFA. Multi-user / SSO / SAML / LDAP / MFA / audit are commercial-tier work.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	_ "modernc.org/sqlite"
)

const schemaDDL = `
CREATE TABLE IF NOT EXISTS users (
  id            INTEGER PRIMARY KEY,
  username      TEXT    NOT NULL UNIQUE,
  password_hash TEXT    NOT NULL,
  created_at    INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
  id         TEXT    PRIMARY KEY,
  user_id    INTEGER NOT NULL,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS ix_sessions_expires ON sessions(expires_at);
CREATE TABLE IF NOT EXISTS api_tokens (
  id         INTEGER PRIMARY KEY,
  label      TEXT    NOT NULL,
  token_hash TEXT    NOT NULL UNIQUE,
  created_at INTEGER NOT NULL,
  revoked_at INTEGER
);
`

// Store wraps the auth SQLite handle.
type Store struct{ db *sql.DB }

// Open opens (creating if needed) the auth DB. The path is typically
// <policyRoot>/_auth.db. WAL is enabled for safe concurrent reads alongside
// the writes the dashboard performs.
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("auth.Open: empty path")
	}
	dsn := "file:" + filepath.Clean(path) + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("sql.Open: %w", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	if _, err := db.Exec(schemaDDL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// --- users ---

// ErrUserExists indicates the username is taken.
var ErrUserExists = errors.New("user already exists")

// ErrUserNotFound indicates no user by that name.
var ErrUserNotFound = errors.New("user not found")

// ErrBadPassword indicates the supplied password doesn't match the stored hash.
var ErrBadPassword = errors.New("invalid credentials")

// MinPasswordLen is the v0.1.0 minimum. Commercial-tier adds full policy.
const MinPasswordLen = 8

// MinUsernameLen + MaxUsernameLen bound usernames to typeable lengths.
const (
	MinUsernameLen = 2
	MaxUsernameLen = 64
)

// UserCount returns how many users exist; used by setup-token bootstrap to
// decide whether `/setup` is open.
func (s *Store) UserCount() (int, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// CreateUser validates inputs, bcrypt-hashes the password (cost 12), inserts
// the row. Returns ErrUserExists if the username is taken.
func (s *Store) CreateUser(username, password string) (int64, error) {
	username = strings.TrimSpace(username)
	if len(username) < MinUsernameLen || len(username) > MaxUsernameLen {
		return 0, fmt.Errorf("username must be %d-%d chars", MinUsernameLen, MaxUsernameLen)
	}
	if len(password) < MinPasswordLen {
		return 0, fmt.Errorf("password must be at least %d chars", MinPasswordLen)
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), 12)
	if err != nil {
		return 0, fmt.Errorf("bcrypt: %w", err)
	}
	res, err := s.db.Exec(
		`INSERT INTO users (username, password_hash, created_at) VALUES (?, ?, ?)`,
		username, string(hash), time.Now().Unix(),
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return 0, ErrUserExists
		}
		return 0, err
	}
	return res.LastInsertId()
}

// VerifyPassword looks up the user, runs bcrypt.CompareHashAndPassword (which
// is constant-time against the hash), returns ErrUserNotFound or ErrBadPassword
// on failure. Both errors deliberately surface the same generic message to the
// caller - caller maps both to a single "invalid credentials" UI message.
func (s *Store) VerifyPassword(username, password string) (int64, error) {
	var id int64
	var hash string
	err := s.db.QueryRow(`SELECT id, password_hash FROM users WHERE username = ?`, username).Scan(&id, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		// Run a fake bcrypt to keep timing constant regardless of whether the
		// user exists. Mitigates user-enumeration via timing attacks.
		_ = bcrypt.CompareHashAndPassword([]byte("$2a$12$"+strings.Repeat("x", 53)), []byte(password))
		return 0, ErrUserNotFound
	}
	if err != nil {
		return 0, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return 0, ErrBadPassword
	}
	return id, nil
}

// --- sessions ---

// SessionTTLDefault is the default cookie lifetime. Operator-overridable.
const SessionTTLDefault = 12 * time.Hour

// NewSession generates a 32-byte random session ID (hex-encoded), inserts the
// session row, returns the ID to set as a cookie. Caller is responsible for
// passing a sensible TTL; SessionTTLDefault is exported for that. A non-positive
// TTL is honored verbatim - useful for tests that want to inject an
// already-expired session.
func (s *Store) NewSession(userID int64, ttl time.Duration) (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	id := hex.EncodeToString(b[:])
	now := time.Now()
	_, err := s.db.Exec(
		`INSERT INTO sessions (id, user_id, created_at, expires_at) VALUES (?, ?, ?, ?)`,
		id, userID, now.Unix(), now.Add(ttl).Unix(),
	)
	if err != nil {
		return "", err
	}
	return id, nil
}

// LookupSession resolves a session cookie to a user ID. Expired sessions return
// ErrSessionNotFound (and are not garbage-collected here - caller may call
// SweepExpiredSessions periodically).
func (s *Store) LookupSession(id string) (int64, error) {
	var userID int64
	var expiresAt int64
	err := s.db.QueryRow(
		`SELECT user_id, expires_at FROM sessions WHERE id = ?`, id,
	).Scan(&userID, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrSessionNotFound
	}
	if err != nil {
		return 0, err
	}
	if time.Now().Unix() >= expiresAt {
		// Lazy cleanup - delete the expired session as we discover it.
		_, _ = s.db.Exec(`DELETE FROM sessions WHERE id = ?`, id)
		return 0, ErrSessionNotFound
	}
	return userID, nil
}

// ErrSessionNotFound indicates the session was missing or expired.
var ErrSessionNotFound = errors.New("session not found")

// DeleteSession removes a session row (used by logout).
func (s *Store) DeleteSession(id string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	return err
}

// SweepExpiredSessions drops all expired session rows. Safe to call
// opportunistically (e.g., on container start).
func (s *Store) SweepExpiredSessions() error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE expires_at < ?`, time.Now().Unix())
	return err
}

// --- API tokens (agent stats layer) ---

// APIToken is one issued token's metadata; the secret itself is never stored.
type APIToken struct {
	ID        int64
	Label     string
	CreatedAt int64
	RevokedAt int64 // 0 = active
}

const apiTokenPrefix = "nbg_"

func hashAPIToken(tok string) string {
	sum := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(sum[:])
}

// CreateAPIToken mints a high-entropy bearer token, stores only its SHA-256
// hash, and returns the secret - shown once, never recoverable. High-entropy
// random tokens need no bcrypt: the hash is unguessable and lookup stays
// index-friendly and constant-cost per request.
func (s *Store) CreateAPIToken(label string) (string, error) {
	label = strings.TrimSpace(label)
	if label == "" {
		return "", errors.New("token label required")
	}
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	tok := apiTokenPrefix + hex.EncodeToString(b[:])
	if _, err := s.db.Exec(
		`INSERT INTO api_tokens (label, token_hash, created_at) VALUES (?, ?, ?)`,
		label, hashAPIToken(tok), time.Now().Unix(),
	); err != nil {
		return "", err
	}
	return tok, nil
}

// VerifyAPIToken reports whether tok is a known, unrevoked token.
func (s *Store) VerifyAPIToken(tok string) (bool, error) {
	var revoked sql.NullInt64
	err := s.db.QueryRow(
		`SELECT revoked_at FROM api_tokens WHERE token_hash = ?`, hashAPIToken(tok),
	).Scan(&revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return !revoked.Valid, nil
}

// ListAPITokens returns all tokens (active and revoked), newest first.
func (s *Store) ListAPITokens() ([]APIToken, error) {
	rows, err := s.db.Query(
		`SELECT id, label, created_at, COALESCE(revoked_at, 0) FROM api_tokens ORDER BY id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIToken
	for rows.Next() {
		var t APIToken
		if err := rows.Scan(&t.ID, &t.Label, &t.CreatedAt, &t.RevokedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// RevokeAPIToken marks a token revoked (idempotent on already-revoked rows).
func (s *Store) RevokeAPIToken(id int64) error {
	_, err := s.db.Exec(
		`UPDATE api_tokens SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`,
		time.Now().Unix(), id)
	return err
}
