// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package checks

import (
	"os"
	"path/filepath"
	"testing"

	"nimblegate/internal/engine"
)

func arWrite(t *testing.T, root, rel, content string) string {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func arCtx(root string, changed ...string) engine.CheckContext {
	return engine.CheckContext{Trigger: engine.TriggerCLI, ProjectRoot: root, ChangedFiles: changed}
}

func TestApprovedRegistriesOnly_PomRepositoryURLFires(t *testing.T) {
	root := t.TempDir()
	arWrite(t, root, "pom.xml", `<project>
  <repositories>
    <repository>
      <id>central</id>
      <url>https://repo.maven.apache.org/maven2</url>
    </repository>
  </repositories>
</project>`)
	res := ApprovedRegistriesOnly(arCtx(root))
	if res.Outcome != engine.OutcomeWarn {
		t.Fatalf("want WARN, got %v (%s)", res.Outcome, res.Reason)
	}
	if len(res.Hits) != 1 || res.Hits[0].Label != "repo.maven.apache.org" {
		t.Fatalf("unexpected hits: %+v", res.Hits)
	}
}

func TestApprovedRegistriesOnly_PomProjectURLOutsideRepoBlockPasses(t *testing.T) {
	root := t.TempDir()
	arWrite(t, root, "pom.xml", `<project>
  <url>https://example.com/my-project</url>
  <scm><url>https://github.com/acme/repo</url></scm>
</project>`)
	res := ApprovedRegistriesOnly(arCtx(root))
	if res.Outcome != engine.OutcomePass {
		t.Fatalf("want PASS, got %v (%s)", res.Outcome, res.Reason)
	}
}

func TestApprovedRegistriesOnly_GradleShortcutAndURLFire(t *testing.T) {
	root := t.TempDir()
	arWrite(t, root, "build.gradle", `repositories {
    mavenCentral()
    maven { url "https://plugins.gradle.org/m2/" }
}`)
	res := ApprovedRegistriesOnly(arCtx(root))
	if res.Outcome != engine.OutcomeWarn {
		t.Fatalf("want WARN, got %v (%s)", res.Outcome, res.Reason)
	}
	if len(res.Hits) != 2 {
		t.Fatalf("want 2 hits, got %+v", res.Hits)
	}
}

func TestApprovedRegistriesOnly_NpmrcAndPipFire(t *testing.T) {
	root := t.TempDir()
	arWrite(t, root, ".npmrc", "registry=https://registry.npmjs.org/\n@acme:registry=https://npm.pkg.github.com/\n")
	arWrite(t, root, "requirements.txt", "--extra-index-url https://pypi.org/simple\nrequests==2.31.0\n")
	res := ApprovedRegistriesOnly(arCtx(root))
	if res.Outcome != engine.OutcomeWarn {
		t.Fatalf("want WARN, got %v (%s)", res.Outcome, res.Reason)
	}
	if len(res.Hits) != 3 {
		t.Fatalf("want 3 hits, got %+v", res.Hits)
	}
}

func TestApprovedRegistriesOnly_AllowlistedHostPasses(t *testing.T) {
	root := t.TempDir()
	arWrite(t, root, ".appframes/_canonical/approved-registries.toml", "[registries]\nmirror = \"registry.corp.example.com\"\n")
	arWrite(t, root, "pom.xml", `<project><repositories><repository>
  <url>https://registry.corp.example.com/repository/maven-public</url>
</repository></repositories></project>`)
	res := ApprovedRegistriesOnly(arCtx(root))
	if res.Outcome != engine.OutcomePass {
		t.Fatalf("want PASS, got %v (%s)", res.Outcome, res.Reason)
	}
}

func TestApprovedRegistriesOnly_AllowlistPresentUnlistedHostFires(t *testing.T) {
	root := t.TempDir()
	arWrite(t, root, ".appframes/_canonical/approved-registries.toml", "[registries]\nmirror = \"registry.corp.example.com\"\n")
	arWrite(t, root, ".npmrc", "registry=https://registry.npmjs.org/\n")
	res := ApprovedRegistriesOnly(arCtx(root))
	if res.Outcome != engine.OutcomeWarn {
		t.Fatalf("want WARN, got %v (%s)", res.Outcome, res.Reason)
	}
}

func TestApprovedRegistriesOnly_AllowlistedGradleShortcutPasses(t *testing.T) {
	root := t.TempDir()
	arWrite(t, root, ".appframes/_canonical/approved-registries.toml", "[registries]\ncentral = \"mavenCentral\"\n")
	arWrite(t, root, "build.gradle", "repositories { mavenCentral() }\n")
	res := ApprovedRegistriesOnly(arCtx(root))
	if res.Outcome != engine.OutcomePass {
		t.Fatalf("want PASS, got %v (%s)", res.Outcome, res.Reason)
	}
}

func TestApprovedRegistriesOnly_PrivateHostsPass(t *testing.T) {
	root := t.TempDir()
	arWrite(t, root, ".npmrc", "registry=http://localhost:4873/\n")
	arWrite(t, root, "pip.conf", "[global]\nindex-url = http://repo:8081/repository/pypi/simple\n")
	res := ApprovedRegistriesOnly(arCtx(root))
	if res.Outcome != engine.OutcomePass {
		t.Fatalf("want PASS, got %v (%s)", res.Outcome, res.Reason)
	}
}

func TestApprovedRegistriesOnly_DisableMarkers(t *testing.T) {
	root := t.TempDir()
	arWrite(t, root, ".npmrc", "# appframes:disable-next-line commands/approved-registries-only\nregistry=https://registry.npmjs.org/\n")
	arWrite(t, root, "build.gradle", "// appframes:disable commands/approved-registries-only\nrepositories { mavenCentral() }\n")
	res := ApprovedRegistriesOnly(arCtx(root))
	if res.Outcome != engine.OutcomePass {
		t.Fatalf("want PASS, got %v (%s)", res.Outcome, res.Reason)
	}
}

func TestApprovedRegistriesOnly_PreCommitEmptyChangedFilesPasses(t *testing.T) {
	root := t.TempDir()
	arWrite(t, root, ".npmrc", "registry=https://registry.npmjs.org/\n")
	res := ApprovedRegistriesOnly(engine.CheckContext{Trigger: engine.TriggerPreCommit, ProjectRoot: root})
	if res.Outcome != engine.OutcomePass {
		t.Fatalf("want PASS, got %v (%s)", res.Outcome, res.Reason)
	}
}

func TestApprovedRegistriesOnly_ChangedFilesOnlyThoseScanned(t *testing.T) {
	root := t.TempDir()
	arWrite(t, root, ".npmrc", "registry=https://registry.npmjs.org/\n")
	other := arWrite(t, root, "requirements.txt", "requests==2.31.0\n")
	res := ApprovedRegistriesOnly(arCtx(root, other))
	if res.Outcome != engine.OutcomePass {
		t.Fatalf("want PASS, got %v (%s)", res.Outcome, res.Reason)
	}
}
