package commands

import "testing"

// The dashboard's add handler has rejected traversal names since v0.1.0; the
// CLI accepted them, so `gateway add --name ../evil` registered a repo whose
// policy dir and bare repo landed outside the configured roots. Every CLI
// entry point that turns --name into a path now runs the same predicate.
func TestGatewayCLIRejectsUnsafeRepoNames(t *testing.T) {
	bad := []string{"../evil", "../../tmp/pwned", ".", "..", "a/b", "bad name!", "_repos", ""}
	for _, name := range bad {
		if rc := gatewayAdd([]string{"--name", name, "--upstream", "https://example.com/x.git", "--no-import"}); rc == 0 {
			t.Errorf("gateway add accepted unsafe name %q (rc=0)", name)
		}
		for _, cmd := range []func([]string) int{gatewayArchive, gatewayRestore, gatewayRescan} {
			if rc := cmd([]string{"--name", name}); rc == 0 {
				t.Errorf("a lifecycle command accepted unsafe name %q (rc=0)", name)
			}
		}
	}
}

func TestGatewayCLIAcceptsOrdinaryRepoNames(t *testing.T) {
	for _, name := range []string{"my-app", "my_app", "app123", "a.b-c_9"} {
		if !validRepoName(name) || name == "_repos" {
			t.Errorf("ordinary name %q rejected", name)
		}
	}
}
