// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package whitelist

import (
	"regexp"

	"nimblegate/internal/glob"
)

// compileGlob wraps internal/glob.Compile so the call site stays the same
// after the shared glob package was extracted.
func compileGlob(g string) (*regexp.Regexp, error) {
	return glob.Compile(g)
}
