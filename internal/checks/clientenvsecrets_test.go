// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"strings"
	"testing"

	"nimblegate/internal/engine"
)

func runClientEnv(t *testing.T, rel, body string) engine.CheckResult {
	t.Helper()
	root := t.TempDir()
	writeSource(t, root, rel, body)
	return NoSecretInClientEnv(engine.CheckContext{
		Trigger:      engine.TriggerCLI,
		ProjectRoot:  root,
		ExcludedDirs: DefaultExcludes(),
	})
}

func TestNoSecretInClientEnv_Fires(t *testing.T) {
	cases := []struct{ name, rel, body, want string }{
		{"vite openai key in code", "src/chat.ts", "const key = import.meta.env.VITE_OPENAI_API_KEY\n", "VITE_OPENAI_API_KEY"},
		{"vite stripe secret", "src/pay.js", "stripe(import.meta.env.VITE_STRIPE_SECRET_KEY)\n", "VITE_STRIPE_SECRET_KEY"},
		{"next public service role", "lib/db.ts", "createClient(url, process.env.NEXT_PUBLIC_SUPABASE_SERVICE_ROLE_KEY!)\n", "NEXT_PUBLIC_SUPABASE_SERVICE_ROLE_KEY"},
		{"bracket access", "src/a.ts", "const k = process.env['REACT_APP_ANTHROPIC_API_KEY']\n", "REACT_APP_ANTHROPIC_API_KEY"},
		{"expo resend", "app/mail.ts", "fetch(u, {headers: {Authorization: process.env.EXPO_PUBLIC_RESEND_API_KEY}})\n", "EXPO_PUBLIC_RESEND_API_KEY"},
		{"nuxt password", "server/x.ts", "const p = env.NUXT_PUBLIC_DB_PASSWORD\n", "NUXT_PUBLIC_DB_PASSWORD"},
		{".env.example key", ".env.example", "VITE_OPENAI_API_KEY=\nVITE_APP_NAME=demo\n", "VITE_OPENAI_API_KEY"},
		{"dockerfile arg", "Dockerfile", "ARG VITE_STRIPE_SECRET_KEY\nRUN npm run build\n", "VITE_STRIPE_SECRET_KEY"},
		{"workflow env", ".github/workflows/build.yml", "    env:\n      VITE_DATABASE_URL: ${{ secrets.DB }}\n", "VITE_DATABASE_URL"},
		{"compose list", "docker-compose.yml", "    environment:\n      - NEXT_PUBLIC_JWT_SECRET=${JWT}\n", "NEXT_PUBLIC_JWT_SECRET"},
		{"sveltekit static public", "src/routes/+page.svelte", "<script>\nimport { PUBLIC_SUPABASE_URL, PUBLIC_STRIPE_SECRET } from '$env/static/public';\n</script>\n", "PUBLIC_STRIPE_SECRET"},
		{"sveltekit multi-line import", "src/lib/ai.ts", "import {\n  PUBLIC_OPENAI_KEY\n} from '$env/static/public'\n", "PUBLIC_OPENAI_KEY"},
		{"private key", "src/sign.ts", "sign(data, import.meta.env.VITE_PRIVATE_KEY)\n", "VITE_PRIVATE_KEY"},
		{"vite define", "vite.config.ts", "define: { 'import.meta.env.VITE_SENDGRID_TOKEN': JSON.stringify(t) }\n", "VITE_SENDGRID_TOKEN"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := runClientEnv(t, c.rel, c.body)
			if got.Outcome != engine.OutcomeBlock {
				t.Fatalf("outcome = %s, want BLOCK; reason: %s", got.Outcome, got.Reason)
			}
			if !strings.Contains(got.Reason, c.want) {
				t.Errorf("reason %q does not name %s", got.Reason, c.want)
			}
		})
	}
}

func TestNoSecretInClientEnv_Passes(t *testing.T) {
	cases := []struct{ name, rel, body string }{
		{"publishable stripe", "src/pay.js", "loadStripe(import.meta.env.VITE_STRIPE_PUBLISHABLE_KEY)\n"},
		{"supabase anon", "src/db.ts", "createClient(import.meta.env.VITE_SUPABASE_URL, import.meta.env.VITE_SUPABASE_ANON_KEY)\n"},
		{"vapid public key", "src/push.ts", "const k = import.meta.env.VITE_VAPID_PUBLIC_KEY\n"},
		{"recaptcha site key", "app/form.tsx", "sitekey={process.env.NEXT_PUBLIC_RECAPTCHA_SITE_KEY}\n"},
		{"analytics id", "app/ga.tsx", "gtag('config', process.env.NEXT_PUBLIC_GA_MEASUREMENT_ID)\n"},
		{"openai model not a key", "src/chat.ts", "const model = import.meta.env.VITE_OPENAI_MODEL\n"},
		{"server-side name", "server/chat.ts", "const key = process.env.OPENAI_API_KEY\n"},
		{"sveltekit private module", "src/routes/+page.server.ts", "import { STRIPE_SECRET_KEY } from '$env/static/private'\n"},
		{"maps api key is public", "src/map.ts", "const k = import.meta.env.VITE_GOOGLE_MAPS_API_KEY\n"},
		{"docs warn about it", "README.md", "Never name it VITE_OPENAI_API_KEY: `import.meta.env.VITE_OPENAI_API_KEY` ships to the browser.\n"},
		{"rendered docs", "public/docs/env.html", "<code>import.meta.env.VITE_STRIPE_SECRET_KEY</code>\n"},
		{"test file", "src/chat.test.ts", "vi.stubEnv('x', import.meta.env.VITE_OPENAI_API_KEY)\n"},
		{"bare name outside svelte env import", "src/consts.py", "PUBLIC_SECRET_SANTA = True\n"},
		{"definition in plain code", "src/config.ts", "VITE_STRIPE_SECRET_KEY: 'x',\n"},
		{"inside NEXT_PUBLIC_ does not double as PUBLIC_", "src/a.ts", "const u = process.env.NEXT_PUBLIC_SITE_URL\n"},
		{"go code naming the svelte module", "internal/checks/env.go", "// import { env } from '$env/dynamic/public'\n//\t# DASHBOARD-ONLY: PUBLIC_SECRET_TOKEN\n"},
		{"minified bundle", "public/app.min.js", "a=import.meta.env.VITE_STRIPE_SECRET_KEY\n"},
		{"disable next line", "src/pay.js", "// appframes:disable-next-line security/no-secret-in-client-env\nconst k = import.meta.env.VITE_STRIPE_SECRET_KEY\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := runClientEnv(t, c.rel, c.body); got.Outcome != engine.OutcomePass {
				t.Fatalf("outcome = %s, want PASS; reason: %s", got.Outcome, got.Reason)
			}
		})
	}
}

func TestNoSecretInClientEnv_NeverEchoesValue(t *testing.T) {
	got := runClientEnv(t, ".env.production", "VITE_OPENAI_API_KEY=valueMustNotAppear42\n")
	if got.Outcome != engine.OutcomeBlock {
		t.Fatalf("outcome = %s, want BLOCK", got.Outcome)
	}
	if strings.Contains(got.Reason, "valueMustNotAppear42") {
		t.Errorf("reason echoes the value: %s", got.Reason)
	}
}
