# SPDX-License-Identifier: PolyForm-Noncommercial-1.0.0
#
# Prints the CHANGELOG.md section for one version as release notes:
#   awk -v v=0.4.8 -f .github/release-notes.awk CHANGELOG.md
# Wrapped continuation lines are joined so each paragraph and list item is one
# line - GitHub can render a hard wrap as a line break. Prints nothing when the
# version has no section, which the release workflow treats as a failure.
BEGIN { gsub(/\./, "\\.", v) }
function flush() {
  if (buf == "") return
  if (blank && started) print ""
  print buf
  started = 1; blank = 0; buf = ""
}
$0 ~ "^## \\[" v "\\]" { f = 1; next }
f && /^## \[/ { exit }
f {
  if ($0 ~ /^[[:space:]]*$/) { flush(); blank = 1; next }
  if ($0 ~ /^(#|- |\* )/) { flush(); buf = $0; next }
  sub(/^[[:space:]]+/, "")
  buf = (buf == "" ? $0 : buf " " $0)
}
END { flush() }
