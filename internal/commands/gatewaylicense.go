// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package commands

import (
	"net/http"
	"strings"

	"nimblegate/internal/gateway"
)

// licenseBuyURL is the "Get a license" destination - the Lemon Squeezy checkout.
const licenseBuyURL = "https://store.nimblegate.com/checkout/buy/5cae2f3a-0745-474f-a3d8-622e276c88fc"

// licenseHandlers owns POST /settings/license - the write half of the
// self-attestation nudge. Honor system: it records the operator's declaration
// and nothing else (no validation, no network).
type licenseHandlers struct {
	policyRoot string
	token      string
}

// save is POST /settings/license. Form body: `commercial` ("1" when checked)
// and optional `order_ref`. Writes <policy-root>/license.toml, then redirects
// back to the About tab.
func (h licenseHandlers) save(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !csrfOK(r, h.token) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	lic := gateway.License{
		Commercial: r.FormValue("commercial") == "1",
		OrderRef:   strings.TrimSpace(r.FormValue("order_ref")),
	}
	if err := gateway.SaveLicense(h.policyRoot, lic); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = gateway.AppendEvent(h.policyRoot, gateway.Event{
		Event:   "license-attestation-update",
		OK:      true,
		Payload: map[string]any{"commercial": lic.Commercial, "has_order_ref": lic.OrderRef != ""},
	})
	redirectAfterAction(w, r, "/settings?tab=about")
}
