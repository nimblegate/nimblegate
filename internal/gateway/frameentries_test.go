package gateway

import "testing"

// A kit name in the frame-ID allowlist matches nothing. The dangerous case is a
// list made only of such entries: non-empty replaces the empty-means-every-frame
// default, so the repo ends up running no checks at all.
func TestUnresolvableFrameEntries(t *testing.T) {
	cases := []struct {
		name    string
		enabled []string
		want    []string
	}{
		{"empty list is not unresolvable", nil, nil},
		{"real IDs resolve", []string{"security/no-hardcoded-credentials", "git/no-force-push-main"}, nil},
		{"kit names do not", []string{"core", "web-app"}, []string{"core", "web-app"}},
		{"mixed reports only the dead", []string{"security/no-hardcoded-credentials", "core"}, []string{"core"}},
		{"category wildcard resolves", []string{"security/*"}, nil},
		{"wildcard on a bogus category does not", []string{"nosuchcat/*"}, []string{"nosuchcat/*"}},
		{"stale renamed ID", []string{"security/frame-that-was-removed"}, []string{"security/frame-that-was-removed"}},
	}
	for _, c := range cases {
		got := UnresolvableFrameEntries(c.enabled)
		if len(got) != len(c.want) {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: got %v want %v", c.name, got, c.want)
				break
			}
		}
	}
}
