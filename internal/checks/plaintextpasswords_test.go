// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"strings"
	"testing"

	"nimblegate/internal/engine"
)

// This file is the plaintext-password frame's own fixture corpus: every
// password in it is fabricated.
// appframes:disable security/no-plaintext-passwords

func runPlaintextPw(t *testing.T, rel, body string) engine.CheckResult {
	t.Helper()
	root := t.TempDir()
	writeSource(t, root, rel, body)
	return NoPlaintextPasswords(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
}

func TestNoPlaintextPasswords_Fires(t *testing.T) {
	cases := []struct {
		name, rel, body, label string
	}{
		{"url userinfo", "config/app.yml", "database_url: postgres://app:Tr0ub4dor3x@db.internal:5432/app\n", "password in URL"},
		{"url userinfo https", "deploy.sh", "git push https://deploy:s3cretValue9@git.example-corp.io/x.git\n", "password in URL"},
		{"mysql attached -p", "run.sh", "mysql -u root -pS3cretPw9 appdb < dump.sql\n", "mysql password flag"},
		{"mysql --password=", "run.sh", "mysqldump --user=root --password=S3cretPw9 appdb\n", "mysql password flag"},
		{"sshpass", "run.sh", "sshpass -p hunter2x ssh root@10.0.0.5\n", "sshpass password"},
		{"curl -u", "run.sh", "curl -u admin:Pa55word9 https://api.internal/v1\n", "curl user:password"},
		{"PGPASSWORD", "run.sh", "PGPASSWORD=Pa55word9 psql -h db -U app\n", "password environment variable"},
		{"docker login -p", "ci.sh", "docker login -u bot -p Pa55word9 registry.internal\n", "docker login password"},
		{"redis-cli -a", "ci.sh", "redis-cli -h cache -a Pa55word9 ping\n", "redis-cli password"},
		{"claude local settings", ".claude/settings.local.json",
			`{"permissions":{"allow":["Bash(mysql -u root -pS3cretPw9 appdb:*)","Bash(sshpass -p hunter2x ssh root@10.0.0.5:*)"]}}` + "\n", "mysql password flag"},
		{".pgpass", "home/.pgpass", "db.internal:5432:app:app:Tr0ub4dor3x\n", ".pgpass entry"},
		{".netrc", "home/.netrc", "machine api.internal login bot password Tr0ub4dor3x\n", ".netrc password"},
		{".npmrc token", ".npmrc", "//registry.npmjs.org/:_authToken=npm_abcdefghijklmnop1234\n", ".npmrc auth token"},
		{".pypirc", ".pypirc", "[pypi]\nusername = __token__\npassword = pypi-AgEIcHlwaS5vcmcCJ1234\n", ".pypirc password"},
		{".my.cnf", ".my.cnf", "[client]\nuser = root\npassword = Tr0ub4dor3x\n", "MySQL option file password"},
		{"docker config", "ci/.docker/config.json", `{"auths":{"registry.internal":{"auth":"Ym90OlBhNTV3b3JkOQ=="}}}` + "\n", "Docker registry auth"},
		{".vault-token", ".vault-token", "hvs.CAESIabcdefghijklmnopqrstuvwxyz\n", "Vault token file"},
		{"rails master key", "config/master.key", "0123456789abcdef0123456789abcdef\n", "Rails master key"},
		{"credential file in test dir", "tests/fixtures/.pgpass", "db.internal:5432:app:app:Tr0ub4dor3x\n", ".pgpass entry"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := runPlaintextPw(t, c.rel, c.body)
			if got.Outcome != engine.OutcomeBlock {
				t.Fatalf("outcome = %s, want BLOCK; reason: %s", got.Outcome, got.Reason)
			}
			if !strings.Contains(got.Reason, c.label) {
				t.Errorf("reason missing %q: %s", c.label, got.Reason)
			}
			for _, secret := range []string{"Tr0ub4dor3x", "s3cretValue9", "S3cretPw9", "hunter2x", "Pa55word9", "npm_abcdef", "pypi-AgE", "Ym90Ol", "hvs.CAES", "0123456789abcdef"} {
				if strings.Contains(got.Reason, secret) {
					t.Errorf("REDACTION FAILURE: %q in reason: %s", secret, got.Reason)
				}
			}
		})
	}
}

func TestNoPlaintextPasswords_Passes(t *testing.T) {
	cases := []struct{ name, rel, body string }{
		{"url env placeholder", "config/app.yml", "url: postgres://app:${DB_PASSWORD}@db.internal/app\n"},
		{"url angle placeholder", "README.md", "Use `postgres://user:<password>@host/db`.\n"},
		{"url word placeholder", "README.md", "postgres://user:password@db.internal/app\n"},
		{"url localhost dev default", "docker-compose.yml", "DATABASE_URL: postgres://postgres:devpass1@localhost:5432/app\n"},
		{"url compose same user/pass", "docker-compose.yml", "DATABASE_URL: postgres://postgres:postgres@db:5432/app\n"},
		{"url example host", "docs/setup.md", "https://alice:Tr0ub4dor3x@git.example.com/repo.git\n"},
		{"url no password", "README.md", "ssh://git@github.com/owner/repo.git\n"},
		{"url template", "tmpl.go", "fmt.Sprintf(\"postgres://%s:%s@%s/%s\", u, p, h, d)\n"},
		{"mysql prompts", "run.sh", "mysql -u root -p appdb\n"},
		{"mysql port capital P", "run.sh", "mysql -u root -P3306 -h db appdb\n"},
		{"mysql env var", "run.sh", "mysql -u root -p\"$MYSQL_PASSWORD\" appdb\n"},
		{"sshpass env", "run.sh", "sshpass -e ssh root@10.0.0.5\n"},
		{"curl token var", "run.sh", "curl -u \"$USER:$TOKEN\" https://api.internal\n"},
		{"PGPASSWORD var", "run.sh", "PGPASSWORD=$DB_PASS psql -h db\n"},
		{"npmrc env token", ".npmrc", "//registry.npmjs.org/:_authToken=${NPM_TOKEN}\n"},
		{"pypirc no password", ".pypirc", "[pypi]\nusername = __token__\n"},
		{"my.cnf no password", ".my.cnf", "[client]\nuser = root\n"},
		{"docker config creds store", "ci/.docker/config.json", `{"credsStore":"desktop","auths":{"registry.internal":{}}}` + "\n"},
		{"empty vault token", ".vault-token", "\n"},
		{"master.key placeholder", "config/master.key", "put-the-real-key-here\n"},
		{"fake marker", "run.sh", "sshpass -p fakepass123 ssh root@10.0.0.5\n"},
		{"test file url", "internal/relay_test.go", "{\"http://user:tok123@host:3000/org/repo.git\", \"http://***@host:3000/org/repo.git\"},\n"},
		{"fixture dir command", "fixtures/run.sh", "sshpass -p hunter2x ssh root@10.0.0.5\n"},
		{"doc constant placeholder", "docs/setup.md", "--upstream \"http://GITEA_USER:GITEA_TOKEN@gitea.corp.io:3000/you/repo.git\"\n"},
		{"doc ip range", "docs/setup.md", "http://bot:Tr0ub4dor3x@192.0.2.20:3000/you/repo.git\n"},
		{"settings no secrets", ".claude/settings.local.json", `{"permissions":{"allow":["Bash(go test:*)","Bash(git push:*)"]}}` + "\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := runPlaintextPw(t, c.rel, c.body)
			if got.Outcome != engine.OutcomePass {
				t.Fatalf("outcome = %s, want PASS; reason: %s", got.Outcome, got.Reason)
			}
		})
	}
}

func TestNoPlaintextPasswords_DisableMarkers(t *testing.T) {
	got := runPlaintextPw(t, "run.sh", "# appframes:disable-next-line security/no-plaintext-passwords\nsshpass -p hunter2x ssh root@10.0.0.5\n")
	if got.Outcome != engine.OutcomePass {
		t.Fatalf("next-line marker: outcome = %s, want PASS; reason: %s", got.Outcome, got.Reason)
	}
	got = runPlaintextPw(t, "run.sh", "# appframes:disable security/no-plaintext-passwords\nsshpass -p hunter2x ssh root@10.0.0.5\n")
	if got.Outcome != engine.OutcomePass {
		t.Fatalf("file marker: outcome = %s, want PASS; reason: %s", got.Outcome, got.Reason)
	}
}

func TestNoPlaintextPasswords_PreCommitScope(t *testing.T) {
	root := t.TempDir()
	writeSource(t, root, "run.sh", "sshpass -p hunter2x ssh root@10.0.0.5\n")
	got := NoPlaintextPasswords(engine.CheckContext{Trigger: engine.TriggerPreCommit, ProjectRoot: root, ExcludedDirs: DefaultExcludes()})
	if got.Outcome != engine.OutcomePass {
		t.Fatalf("pre-commit with nothing staged: outcome = %s, want PASS", got.Outcome)
	}
}
