// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"os"
	"path/filepath"
	"testing"

	"nimblegate/internal/engine"
)

func TestNoDatabaseDumpInRepo_MysqldumpHeaderWarns(t *testing.T) {
	root := t.TempDir()
	piiWrite(t, root, "backup/prod.sql",
		"-- MySQL dump 10.13  Distrib 8.0.36\n--\n-- Host: db.internal    Database: prod\nINSERT INTO users VALUES (1,'a');\n")
	res := NoDatabaseDumpInRepo(piiCtx(root))
	if res.Outcome != engine.OutcomeWarn {
		t.Fatalf("want WARN, got %v (%s)", res.Outcome, res.Reason)
	}
}

func TestNoDatabaseDumpInRepo_PgDumpHeaderWarns(t *testing.T) {
	root := t.TempDir()
	piiWrite(t, root, "db.sql", "--\n-- PostgreSQL database dump\n--\nCOPY public.users FROM stdin;\n")
	res := NoDatabaseDumpInRepo(piiCtx(root))
	if res.Outcome != engine.OutcomeWarn {
		t.Fatalf("want WARN, got %v (%s)", res.Outcome, res.Reason)
	}
}

func TestNoDatabaseDumpInRepo_BinaryMagicWarns(t *testing.T) {
	root := t.TempDir()
	pg := append([]byte("PGDMP"), []byte{0x01, 0x0e, 0x00, 0x04}...)
	path := filepath.Join(root, "snapshot.dump")
	if err := os.WriteFile(path, pg, 0o644); err != nil {
		t.Fatal(err)
	}
	sq := append([]byte("SQLite format 3\x00"), make([]byte, 32)...)
	if err := os.WriteFile(filepath.Join(root, "app.db"), sq, 0o644); err != nil {
		t.Fatal(err)
	}
	res := NoDatabaseDumpInRepo(piiCtx(root))
	if res.Outcome != engine.OutcomeWarn {
		t.Fatalf("want WARN, got %v (%s)", res.Outcome, res.Reason)
	}
	if len(res.Hits) != 2 {
		t.Fatalf("want 2 hits (PGDMP + SQLite), got %+v", res.Hits)
	}
}

func TestNoDatabaseDumpInRepo_HandWrittenSeedPasses(t *testing.T) {
	root := t.TempDir()
	piiWrite(t, root, "db/seeds.sql",
		"-- seed data for local development\nINSERT INTO plans (name) VALUES ('free'), ('pro');\n")
	res := NoDatabaseDumpInRepo(piiCtx(root))
	if res.Outcome != engine.OutcomePass {
		t.Fatalf("want PASS, got %v (%s)", res.Outcome, res.Reason)
	}
}

func TestNoDatabaseDumpInRepo_DisableMarkerHonored(t *testing.T) {
	root := t.TempDir()
	piiWrite(t, root, "fixtures/known.sql",
		"-- appframes:disable security/no-database-dump-in-repo\n-- MySQL dump 10.13\nINSERT INTO t VALUES (1);\n")
	res := NoDatabaseDumpInRepo(piiCtx(root))
	if res.Outcome != engine.OutcomePass {
		t.Fatalf("want PASS, got %v (%s)", res.Outcome, res.Reason)
	}
}
