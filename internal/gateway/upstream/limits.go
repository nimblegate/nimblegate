// SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0

package upstream

// maxUpstreamResponseBytes caps a single API response from a git host. These
// are JSON replies about pull requests and comments - a few hundred KB at the
// outside - so 4 MiB is generous. Without a cap the daemon reads whatever the
// far end sends, which is the one remaining unbounded read in the relay path.
const maxUpstreamResponseBytes int64 = 4 << 20
