// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"nimblegate/internal/engine"
)

func runKeyCheck(t *testing.T, rel, content string) engine.CheckResult {
	t.Helper()
	root := t.TempDir()
	writeSource(t, root, rel, content)
	return NoPrivateKeysInRepo(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
}

func TestNoPrivateKeysInRepo_RSAPrivateKeyBlocks(t *testing.T) {
	body := `-----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAKCAQEAyL...redacted...
-----END RSA PRIVATE KEY-----
`
	got := runKeyCheck(t, "keys/server.pem", body)
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("outcome = %s, want BLOCK", got.Outcome)
	}
	// The .pem extension would also fire - that's fine, this is mixed.
	if !strings.Contains(got.Reason, "PEM RSA private key") {
		t.Errorf("reason missing label: %s", got.Reason)
	}
	// REDACTION: the actual base64 lines must not appear.
	if strings.Contains(got.Reason, "MIIEpAIBAAKCAQEAyL") {
		t.Errorf("REDACTION FAILURE: key bytes in reason: %s", got.Reason)
	}
}

func TestNoPrivateKeysInRepo_OpenSSHPrivateKeyBlocks(t *testing.T) {
	body := "-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjE=\n-----END OPENSSH PRIVATE KEY-----\n"
	got := runKeyCheck(t, "id_ed25519", body)
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("outcome = %s, want BLOCK", got.Outcome)
	}
}

func TestNoPrivateKeysInRepo_ECPrivateKeyBlocks(t *testing.T) {
	body := "-----BEGIN EC PRIVATE KEY-----\nMHcCAQEEIM/...redacted...\n-----END EC PRIVATE KEY-----\n"
	got := runKeyCheck(t, "ec.txt", body)
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("outcome = %s, want BLOCK; reason: %s", got.Outcome, got.Reason)
	}
	if !strings.Contains(got.Reason, "PEM EC private key") {
		t.Errorf("reason missing EC label: %s", got.Reason)
	}
}

func TestNoPrivateKeysInRepo_PGPPrivateKeyBlocks(t *testing.T) {
	body := "-----BEGIN PGP PRIVATE KEY BLOCK-----\nlQVYBGT...\n-----END PGP PRIVATE KEY BLOCK-----\n"
	got := runKeyCheck(t, "secret.asc", body)
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("outcome = %s, want BLOCK", got.Outcome)
	}
}

// TestNoPrivateKeysInRepo_EncryptedPrivateKey - encrypted keys still
// count as private keys (the passphrase is the missing link, not the
// data being non-secret).
func TestNoPrivateKeysInRepo_EncryptedPrivateKey(t *testing.T) {
	body := "-----BEGIN ENCRYPTED PRIVATE KEY-----\nMIIFDjBABg...\n-----END ENCRYPTED PRIVATE KEY-----\n"
	got := runKeyCheck(t, "enc.txt", body)
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("outcome = %s, want BLOCK", got.Outcome)
	}
}

// TestNoPrivateKeysInRepo_CertificateInfoOnly - TLS certs are catalogued.
func TestNoPrivateKeysInRepo_CertificateInfoOnly(t *testing.T) {
	body := "-----BEGIN CERTIFICATE-----\nMIIDXTCCAkWgAwIBAgI...\n-----END CERTIFICATE-----\n"
	root := t.TempDir()
	// Use .txt extension so the file-extension detection doesn't also fire.
	writeSource(t, root, "ca.txt", body)
	got := NoPrivateKeysInRepo(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeInfo {
		t.Errorf("outcome = %s, want INFO (certs catalogued, not blocked); reason: %s",
			got.Outcome, got.Reason)
	}
	if !strings.Contains(got.Reason, "X.509 certificate") {
		t.Errorf("reason missing certificate label: %s", got.Reason)
	}
	if strings.Contains(got.Reason, "REGENERATE") || strings.Contains(got.Fix, "REGENERATE") {
		t.Errorf("INFO path should not use 'REGENERATE' urgency wording")
	}
}

func TestNoPrivateKeysInRepo_FilenameIdRsa(t *testing.T) {
	root := t.TempDir()
	// An empty `id_rsa` file - filename alone is enough.
	writeSource(t, root, "id_rsa", "")
	got := NoPrivateKeysInRepo(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("outcome = %s, want BLOCK on bare id_rsa filename", got.Outcome)
	}
	if !strings.Contains(got.Reason, `SSH private key filename "id_rsa"`) {
		t.Errorf("reason missing filename label: %s", got.Reason)
	}
}

// TestNoPrivateKeysInRepo_IdRsaPubPasses - `.pub` variant is public,
// must NOT BLOCK.
func TestNoPrivateKeysInRepo_IdRsaPubPasses(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "id_rsa.pub", "ssh-rsa AAAAB3NzaC1yc2EA...\n")
	got := NoPrivateKeysInRepo(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS for id_rsa.pub", got.Outcome)
	}
}

func TestNoPrivateKeysInRepo_PEMExtensionBlocks(t *testing.T) {
	root := t.TempDir()
	// Empty .pem file - extension alone triggers.
	writeSource(t, root, "keys/server.pem", "")
	got := NoPrivateKeysInRepo(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("outcome = %s, want BLOCK for .pem extension", got.Outcome)
	}
}

func TestNoPrivateKeysInRepo_KeyExtensionBlocks(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "secret.key", "")
	got := NoPrivateKeysInRepo(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Errorf("outcome = %s, want BLOCK for .key", got.Outcome)
	}
}

// TestNoPrivateKeysInRepo_CrtExtensionInfo - .crt files are catalogued.
func TestNoPrivateKeysInRepo_CrtExtensionInfo(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "ca-bundle.crt", "")
	got := NoPrivateKeysInRepo(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeInfo {
		t.Errorf("outcome = %s, want INFO for .crt", got.Outcome)
	}
}

// TestNoPrivateKeysInRepo_BlockWinsOverInfo - when both fire, BLOCK wins,
// reason surfaces both.
func TestNoPrivateKeysInRepo_BlockWinsOverInfo(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "keys/server.pem",
		`-----BEGIN RSA PRIVATE KEY-----
data
-----END RSA PRIVATE KEY-----
`)
	writeSource(t, root, "ca.crt", "")
	got := NoPrivateKeysInRepo(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s, want BLOCK (BLOCK + INFO → BLOCK)", got.Outcome)
	}
	if !strings.Contains(got.Reason, "private key") {
		t.Errorf("reason missing private-key entry: %s", got.Reason)
	}
	if !strings.Contains(got.Reason, "certificate") {
		t.Errorf("reason should also surface the cert (INFO) when BLOCK is present: %s", got.Reason)
	}
	if !strings.Contains(got.Fix, "REGENERATE") {
		t.Errorf("BLOCK path must keep REGENERATE language: %s", got.Fix)
	}
}

func TestNoPrivateKeysInRepo_PerFileDisableSuppresses(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "tests/fixture.go",
		`# appframes:disable security/no-private-keys-in-repo
-----BEGIN RSA PRIVATE KEY-----
fake-test-fixture
-----END RSA PRIVATE KEY-----
`)
	got := NoPrivateKeysInRepo(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS (per-file disable); reason: %s",
			got.Outcome, got.Reason)
	}
}

func TestNoPrivateKeysInRepo_PerLineDisableSuppresses(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "tests/fixture.go",
		`# appframes:disable-next-line security/no-private-keys-in-repo
-----BEGIN RSA PRIVATE KEY-----
fake
-----END RSA PRIVATE KEY-----
`)
	got := NoPrivateKeysInRepo(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS (per-line disable); reason: %s",
			got.Outcome, got.Reason)
	}
}

func TestNoPrivateKeysInRepo_NoiseDirsExcluded(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "node_modules/dep/key.pem",
		"-----BEGIN RSA PRIVATE KEY-----\nfake\n-----END RSA PRIVATE KEY-----\n")
	got := NoPrivateKeysInRepo(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS (node_modules excluded)", got.Outcome)
	}
}

// TestNoPrivateKeysInRepo_PreCommitEmptyChangesPasses - file-scan scope contract.
func TestNoPrivateKeysInRepo_PreCommitEmptyChangesPasses(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "keys/leak.pem",
		"-----BEGIN RSA PRIVATE KEY-----\nfake\n-----END RSA PRIVATE KEY-----\n")
	got := NoPrivateKeysInRepo(engine.CheckContext{
		Trigger:      engine.TriggerPreCommit,
		ProjectRoot:  root,
		ChangedFiles: nil,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS (pre-commit + empty stage)", got.Outcome)
	}
}

// TestNoPrivateKeysInRepo_PreCommitStagedScansThoseOnly
func TestNoPrivateKeysInRepo_PreCommitStagedScansThoseOnly(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "keys/staged.pem", "")
	writeSource(t, root, "keys/untouched.pem", "")
	got := NoPrivateKeysInRepo(engine.CheckContext{
		Trigger:      engine.TriggerPreCommit,
		ProjectRoot:  root,
		ChangedFiles: []string{filepath.Join(root, "keys/staged.pem")},
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s, want BLOCK", got.Outcome)
	}
	if !strings.Contains(got.Reason, "staged.pem") {
		t.Errorf("missing staged file: %s", got.Reason)
	}
	if strings.Contains(got.Reason, "untouched.pem") {
		t.Errorf("untouched file leaked: %s", got.Reason)
	}
}

// TestNoPrivateKeysInRepo_LargeFileSkipped - over 1MB → assumed binary.
func TestNoPrivateKeysInRepo_LargeFileSkipped(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "big")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Build a >1MB file with a PEM header inside it, but use an extension
	// that isn't in the filename catalog so only content scan would apply.
	big := strings.Repeat("padding ", 200_000) + "\n-----BEGIN RSA PRIVATE KEY-----\nfake\n"
	if err := os.WriteFile(filepath.Join(dir, "blob.bin"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	got := NoPrivateKeysInRepo(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
	if got.Outcome != engine.OutcomePass {
		t.Errorf("outcome = %s, want PASS (>1MB skip); reason: %s",
			got.Outcome, got.Reason)
	}
}

// TestNoPrivateKeysInRepo_PEMHeaderRedactionGuarantee - even if the test
// file contains a long base64-looking body, the reason must mention
// the header label only, never echo the body bytes.
func TestNoPrivateKeysInRepo_RedactionGuarantee(t *testing.T) {
	secretBytes := strings.Repeat("AAAA", 32) // 128 chars of "secret-shape" base64
	body := "-----BEGIN RSA PRIVATE KEY-----\n" + secretBytes + "\n-----END RSA PRIVATE KEY-----\n"
	got := runKeyCheck(t, "k.txt", body)
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s, want BLOCK", got.Outcome)
	}
	if strings.Contains(got.Reason, secretBytes) {
		t.Errorf("REDACTION FAILURE: body bytes in reason: %s", got.Reason)
	}
}
