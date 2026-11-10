package commands

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
)

func TestEmptyFeed_pushURLHintIsUsable(t *testing.T) {
	var b bytes.Buffer
	if err := gwTmpl.ExecuteTemplate(&b, "rows", struct{ Rows []any }{}); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if !strings.Contains(out, "No decisions yet") {
		t.Fatalf("empty-feed block not rendered: %s", out)
	}
	// scp-style git@host:2222/repo reads 2222 as part of the path, not the port.
	if regexp.MustCompile(`(^|[^/])git@&lt;host&gt;:2222/`).MatchString(out) {
		t.Errorf("hint shows the scp-style form with a port: %s", out)
	}
	for _, want := range []string{"ssh://git@&lt;host&gt;:2222/~/&lt;repo&gt;.git", "ssh://git@&lt;host&gt;/srv/gateway/repos/&lt;repo&gt;.git"} {
		if !strings.Contains(out, want) {
			t.Errorf("hint missing %q", want)
		}
	}
}

func TestSSHKeysPage_pushURLHintIsUsable(t *testing.T) {
	var b bytes.Buffer
	if err := sshKeysContentTmpl.ExecuteTemplate(&b, "content", nil); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	if !strings.Contains(out, "Keys authorized to push") {
		t.Fatalf("keys intro not rendered")
	}
	if regexp.MustCompile(`(^|[^/])git@&lt;host&gt;:`).MatchString(out) {
		t.Errorf("keys page shows an scp-style address: %s", out)
	}
	if !strings.Contains(out, "ssh://git@&lt;host&gt;:2222/~/&lt;repo&gt;.git") {
		t.Errorf("keys page missing the Docker push address")
	}
}
