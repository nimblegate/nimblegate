// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"strings"
	"testing"

	"nimblegate/internal/engine"
)

// Fixture corpus for the possible-secret frame: every value is fabricated.
// appframes:disable security/possible-secret
// appframes:disable security/no-plaintext-passwords
// appframes:disable security/no-hardcoded-credentials

func runPossibleSecret(t *testing.T, rel, body string) engine.CheckResult {
	t.Helper()
	root := t.TempDir()
	writeSource(t, root, rel, body)
	return PossibleSecret(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
}

func TestPossibleSecret_Warns(t *testing.T) {
	cases := []struct{ name, rel, body, label string }{
		{"go password assignment", "internal/db/conn.go", `const dbPassword = "Tr0ub4dor3x!"` + "\n", "dbPassword"},
		{"python secret", "app/settings.py", `SECRET_KEY = "k9#Vq2!mZx7Lp4@w"` + "\n", "SECRET_KEY"},
		{"js api key", "src/client.js", `const apiKey = 'Zq8xW2mN4vB6cX1z'` + "\n", "apiKey"},
		{"yaml unquoted", "config/prod.yaml", "  password: Tr0ub4dor3x9\n", "password"},
		{"json client secret", "config/oauth.json", `{"client_secret": "aB3dE5fG7hJ9kL1m"}` + "\n", "client_secret"},
		{"entropy near token word", "src/api.ts", "// auth token for the billing service\nconst x = \"q7Zr2LmX9vKp4Wn8Tb6Yc3Hd\";\n", "high-entropy string"},
		{"entropy near authToken", "src/c.ts", "const authToken = getX(); const k = \"q7Zr2LmX9vKp4Wn8Tb6Yc3Hd\";\n", "high-entropy string"},
		{"entropy bearer header", "src/api.go", `req.Header.Set("Authorization", "Bearer q7Zr2LmX9vKp4Wn8Tb6Yc3Hd")` + "\n", "high-entropy string"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := runPossibleSecret(t, c.rel, c.body)
			if got.Outcome != engine.OutcomeWarn {
				t.Fatalf("outcome = %s, want WARN; reason: %s", got.Outcome, got.Reason)
			}
			if !strings.Contains(got.Reason, c.label) {
				t.Errorf("reason missing %q: %s", c.label, got.Reason)
			}
			for _, v := range []string{"Tr0ub4dor3x", "k9#Vq2", "Zq8xW2", "aB3dE5", "q7Zr2L"} {
				if strings.Contains(got.Reason, v) {
					t.Errorf("REDACTION FAILURE: %q in reason: %s", v, got.Reason)
				}
			}
		})
	}
}

func TestPossibleSecret_Quiet(t *testing.T) {
	cases := []struct{ name, rel, body string }{
		{"env lookup", "app/settings.py", `SECRET_KEY = os.environ["SECRET_KEY"]` + "\n"},
		{"process env", "src/client.js", "const apiKey = process.env.API_KEY\n"},
		{"placeholder", "config/app.yaml", "password: changeme\n"},
		{"angle placeholder", "config/app.yaml", "password: \"<your-password>\"\n"},
		{"one char class", "config/app.yaml", "password_min_length: \"12345678\"\n"},
		{"value names the field", "src/form.js", `const passwordField = "password_confirmation"` + "\n"},
		{"tokenizer is not a token", "train.py", `tokenizer = "bert-base-uncased-v2"` + "\n"},
		{"public key name", "src/k.js", `const publicKey = "Zq8xW2mN4vB6cX1z"` + "\n"},
		{"fetch credentials option", "src/load.js", `fetch(url,{credentials:"same-origin"}).then(r => r.text())` + "\n"},
		{"lowercase option value", "config/app.yaml", "auth_token_mode: \"rotate-on-login\"\n"},
		{"header name near token", "aws.go", `awsIMDSv2SessionTokenHeader = "X-aws-ec2-metadata-token"` + "\n"},
		{"dotted key near token", "google.go", `"oauth2.google.tokenSource": "compute-metadata",` + "\n"},
		{"camelcase identifier", "zsys.go", "// private api\nprocQueryServiceConfig2W = modadvapi32.NewProc(\"QueryServiceConfig2W\")\n"},
		{"lowercase bearer word", "README.md", "BEDROCK_AUTH=bearer BEDROCK_MODEL=us.openai.gpt-5.6-luna go run ./x\n"},
		{"tokenizer model name", "probe.py", "tok = AutoTokenizer.from_pretrained(\"BAAI/bge-reranker-v2-m3\")\n"},
		{"token alphabet", "gen.js", "function generateToken() {\n  const chars = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';\n"},
		{"minified bundle line", "app/immutable/nodes/2.BXnGRQD8.js", "var token=1;" + strings.Repeat("import{c as oe,a as d}from\"../chunks/raksZQmW.js\";", 12) + "x=\"q7Zr2LmX9vKp4Wn8Tb6Yc3Hd\"\n"},
		{"svelte-kit output", "web/.svelte-kit/output/a.js", "const apiKey = 'Zq8xW2mN4vB6cX1z'\n"},
		{"virtualenv", "svc/.venv/lib/python3.13/site-packages/x/a.py", "API_KEY = \"Zq8xW2mN4vB6cX1z\"\n"},
		{"camelcase type name near credentials", "api.json", "\"description\": \"client credentials grant\",\n\"id\": \"GoogleCloudApihubV1Oauth2ClientCredentialsConfig\",\n"},
		{"lone key", "src/map.js", `const key = "Zq8xW2mN4vB6cX1z"` + "\n"},
		{"identifier value", "config/app.yaml", "password: DB_PASSWORD\n"},
		{"unquoted in code", "src/a.go", "token := getToken(ctx)\n"},
		{"hash near sha", "build.go", `sha256 := "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08" // token cache` + "\n"},
		{"uuid", "src/a.ts", `const tokenId = "123e4567-e89b-12d3-a456-426614174000"` + "\n"},
		{"integrity", "index.html", `<script integrity="sha384-oqVuAfXRKap7fdgcCY5uykM6+R9GqQ8K/uxy9rx7HNQlGYl1kPzQho1wx4JwY8wC" data-token="x">` + "\n"},
		{"fake marker", "src/a.ts", `const apiKey = "fake-Zq8xW2mN4vB6cX1z"` + "\n"},
		{"test file", "src/a.test.ts", `const apiKey = 'Zq8xW2mN4vB6cX1z'` + "\n"},
		{"testdata dir", "pkg/testdata/conf.yaml", "password: Tr0ub4dor3x9\n"},
		{"lockfile", "package-lock.json", `"integrity": "sha512-Zq8xW2mN4vB6cX1zq7Zr2LmX9vKp4Wn8Tb6Yc3Hd", "token": "x"` + "\n"},
		{"minified", "dist/app.min.js", `var apiKey="Zq8xW2mN4vB6cX1z";` + "\n"},
		{"entropy without context", "src/a.go", `var id = "q7Zr2LmX9vKp4Wn8Tb6Yc3Hd"` + "\n"},
		{"prefixed key left to credentials frame", "src/a.js", `const apiKey = "ghp_1234567890abcdefghijklmnopqrstuvwxyz"` + "\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := runPossibleSecret(t, c.rel, c.body)
			if got.Outcome != engine.OutcomePass {
				t.Fatalf("outcome = %s, want PASS; reason: %s", got.Outcome, got.Reason)
			}
		})
	}
}

func TestPossibleSecret_NextLineMarker(t *testing.T) {
	got := runPossibleSecret(t, "src/a.js", "// appframes:disable-next-line security/possible-secret\nconst apiKey = 'Zq8xW2mN4vB6cX1z'\n")
	if got.Outcome != engine.OutcomePass {
		t.Fatalf("outcome = %s, want PASS; reason: %s", got.Outcome, got.Reason)
	}
}
