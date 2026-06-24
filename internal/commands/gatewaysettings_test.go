// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSettingsPage_SystemTabDefault(t *testing.T) {
	rec := httptest.NewRecorder()
	serveSettings("/etc/nimblegate-gateway/repos", "/srv/gateway/repos", "setup-token", false)(rec, httptest.NewRequest("GET", "/settings", nil))
	b := rec.Body.String()
	for _, want := range []string{
		`class="gw-rail"`,
		`href="/settings" class="gw-railitem gw-bottom active"`,
		// Tab strip with System active by default.
		`href="/settings?tab=system" class="autopr-tab active">System</a>`,
		`href="/settings?tab=display" class="autopr-tab">Display</a>`,
		`href="/settings?tab=about" class="autopr-tab">About</a>`,
		// System info content present.
		`>System info<`, `>Install type<`, `>Policy root<`, `>Auth mode<`,
		`>/etc/nimblegate-gateway/repos<`, `single-admin login`,
	} {
		if !strings.Contains(b, want) {
			t.Errorf("settings system tab missing %q\n", want)
		}
	}
	// Display + About content should NOT render on the System tab.
	for _, unwanted := range []string{
		`data-setting="gwrail"`,
		`>License<`,
	} {
		if strings.Contains(b, unwanted) {
			t.Errorf("settings system tab should NOT contain %q", unwanted)
		}
	}
}

func TestSettingsPage_DisplayTab(t *testing.T) {
	rec := httptest.NewRecorder()
	serveSettings("/etc/nimblegate-gateway/repos", "/srv/gateway/repos", "setup-token", false)(rec, httptest.NewRequest("GET", "/settings?tab=display", nil))
	b := rec.Body.String()
	for _, want := range []string{
		`href="/settings?tab=display" class="autopr-tab active">Display</a>`,
		`data-setting="gwrail"`, `data-setting="gwfeedinterval"`,
		`value="expanded"`, `value="collapsed"`,
		`value="0">Off`, `value="5">every 5s`,
		`data-setting="gwtz"`, `data-setting="gwtc"`, `data-setting="gwday"`,
		`>Timestamp timezone<`,
	} {
		if !strings.Contains(b, want) {
			t.Errorf("settings display tab missing %q", want)
		}
	}
	// System info content should NOT render on the Display tab.
	if strings.Contains(b, `>System info<`) {
		t.Error("settings display tab should NOT contain System info heading")
	}
}

func TestSettingsPage_AboutTab(t *testing.T) {
	rec := httptest.NewRecorder()
	serveSettings("/etc/nimblegate-gateway/repos", "/srv/gateway/repos", "setup-token", false)(rec, httptest.NewRequest("GET", "/settings?tab=about", nil))
	b := rec.Body.String()
	for _, want := range []string{
		`href="/settings?tab=about" class="autopr-tab active">About</a>`,
		`>License<`, `PolyForm Noncommercial 1.0.0`,
		`>Project<`,
		`github.com/nimblegate/nimblegate`,
		`nimblegate.com`,
		`github.com/sponsors/nimblegate`,
		`security@nimblegate.com`,
		`releases page`,
	} {
		if !strings.Contains(b, want) {
			t.Errorf("settings about tab missing %q", want)
		}
	}
	// Display + System content should NOT render on the About tab.
	for _, unwanted := range []string{
		`data-setting="gwrail"`,
		`>System info<`,
	} {
		if strings.Contains(b, unwanted) {
			t.Errorf("settings about tab should NOT contain %q", unwanted)
		}
	}
}
