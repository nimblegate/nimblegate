#!/usr/bin/env bash
#
# nimblegate installer - bootstrap script.
#
# Usage:
#   curl -fsSL <release-url>/install.sh | bash
#   curl -fsSL <release-url>/install.sh | NIMBLEGATE_NONINTERACTIVE=1 bash
#   NIMBLEGATE_BINARY=/path/to/local/nimblegate bash install.sh   # local install
#
# Security note: piping `curl | bash` runs unverified code from the internet.
# To inspect first:
#   curl -fsSL <url>/install.sh -o install.sh
#   less install.sh
#   bash install.sh
#
# Environment variables:
#   NIMBLEGATE_BINARY        Local path to a prebuilt binary. Overrides the
#                            download - useful for local-build installs.
#   NIMBLEGATE_BINARY_URL    Direct download URL of a prebuilt binary. If
#                            neither this nor NIMBLEGATE_BINARY is set, the
#                            script falls back to NIMBLEGATE_RELEASE_BASE (see
#                            below) + os/arch suffix.
#   NIMBLEGATE_RELEASE_BASE  Base URL for prebuilt binary downloads. The
#                            script appends "/nimblegate-<os>-<arch>" to form
#                            the download URL. Default placeholder; set to a
#                            real URL once releases are published.
#   NIMBLEGATE_INSTALL_DIR   Where to place the binary. Default: ~/.appframes/bin.
#   NIMBLEGATE_NONINTERACTIVE  If set to any non-empty value, pass --yes to
#                              `nimblegate setup` (no prompts). Use in CI / scripts.
#
# Exit codes:
#   0  success
#   1  install failed (binary not found, copy/download error, setup failed)
#   2  unsupported OS/arch
#
# What this script does:
#   1. Detects OS/arch
#   2. Resolves binary source: NIMBLEGATE_BINARY > NIMBLEGATE_BINARY_URL > release base
#   3. Installs to NIMBLEGATE_INSTALL_DIR (default ~/.appframes/bin)
#   4. Adds the install dir to PATH via a fenced marker block in ~/.bashrc
#      so the next shell has `nimblegate` available
#   5. Invokes `nimblegate setup` to handle shim install + shim PATH
#
# The two PATH marker blocks (binary path here, shim path from `setup`) are
# both recognized by `nimblegate purge`, which removes them on uninstall.

set -euo pipefail

# --- Configurable defaults ---
# Install dir lives under ~/.appframes/ to match the runtime home directory
# (state, shims, logs). `nimblegate purge` removes ~/.appframes/ wholesale, so
# keeping the binary there is what lets purge clean it up.
NIMBLEGATE_INSTALL_DIR="${NIMBLEGATE_INSTALL_DIR:-$HOME/.appframes/bin}"
NIMBLEGATE_RELEASE_BASE="${NIMBLEGATE_RELEASE_BASE:-https://example.invalid/nimblegate/releases/latest}"
NIMBLEGATE_BINARY="${NIMBLEGATE_BINARY:-}"
NIMBLEGATE_BINARY_URL="${NIMBLEGATE_BINARY_URL:-}"
NIMBLEGATE_NONINTERACTIVE="${NIMBLEGATE_NONINTERACTIVE:-}"

# Markers - must stay in sync with internal/commands/purge.go.
PATH_MARKER_BEGIN="# >>> nimblegate install PATH"
PATH_MARKER_END="# <<< nimblegate install PATH"

# --- Helpers ---

log()  { printf '%s\n' "$*"; }
err()  { printf 'ERROR: %s\n' "$*" >&2; }

detect_os() {
    local uname_s
    uname_s="$(uname -s)"
    case "$uname_s" in
        Linux)  echo "linux" ;;
        Darwin) echo "darwin" ;;
        *)      err "unsupported OS: $uname_s"; exit 2 ;;
    esac
}

detect_arch() {
    local uname_m
    uname_m="$(uname -m)"
    case "$uname_m" in
        x86_64|amd64)  echo "amd64" ;;
        arm64|aarch64) echo "arm64" ;;
        *)             err "unsupported arch: $uname_m"; exit 2 ;;
    esac
}

# resolve_source prints the binary source spec to stdout:
#   "file:<path>"  for local file (NIMBLEGATE_BINARY)
#   "url:<url>"    for download
resolve_source() {
    if [ -n "$NIMBLEGATE_BINARY" ]; then
        if [ ! -f "$NIMBLEGATE_BINARY" ]; then
            err "NIMBLEGATE_BINARY=$NIMBLEGATE_BINARY does not exist"
            exit 1
        fi
        echo "file:$NIMBLEGATE_BINARY"
        return
    fi
    if [ -n "$NIMBLEGATE_BINARY_URL" ]; then
        echo "url:$NIMBLEGATE_BINARY_URL"
        return
    fi
    local os arch
    os="$(detect_os)"
    arch="$(detect_arch)"
    echo "url:$NIMBLEGATE_RELEASE_BASE/nimblegate-$os-$arch"
}

# install_binary copies/downloads from the resolved source to dest.
# dest is the absolute path of the destination file (not a directory).
install_binary() {
    local source="$1"
    local dest="$2"

    case "$source" in
        file:*)
            local src_path="${source#file:}"
            log "Installing from local path: $src_path"
            cp "$src_path" "$dest"
            ;;
        url:*)
            local url="${source#url:}"
            log "Downloading: $url"
            if command -v curl >/dev/null 2>&1; then
                curl -fsSL "$url" -o "$dest"
            elif command -v wget >/dev/null 2>&1; then
                wget -q "$url" -O "$dest"
            else
                err "need curl or wget to download (or set NIMBLEGATE_BINARY to a local path)"
                exit 1
            fi
            ;;
    esac

    if [ ! -s "$dest" ]; then
        err "binary at $dest is empty or missing after install"
        exit 1
    fi
    chmod +x "$dest"
}

# detect_rc_file returns the path to the user's shell rc file.
detect_rc_file() {
    case "${SHELL:-/bin/bash}" in
        */zsh) echo "$HOME/.zshrc" ;;
        *)     echo "$HOME/.bashrc" ;;
    esac
}

# add_install_path_to_rc appends a fenced marker block to the rc file that
# puts the install dir at the front of PATH. Idempotent: if the marker is
# already present, returns without modifying.
add_install_path_to_rc() {
    local rc_path="$1"
    local install_dir="$2"
    local today
    today="$(date '+%Y-%m-%d')"

    if [ -f "$rc_path" ] && grep -q "$PATH_MARKER_BEGIN" "$rc_path" 2>/dev/null; then
        log "  (PATH marker already present in $rc_path; not editing)"
        return 0
    fi

    # Append the block. Ensure rc file has a trailing newline first.
    if [ -f "$rc_path" ] && [ -n "$(tail -c 1 "$rc_path" 2>/dev/null)" ]; then
        printf '\n' >> "$rc_path"
    fi

    {
        printf '\n'
        printf '%s (added %s) >>>\n' "$PATH_MARKER_BEGIN" "$today"
        printf 'export PATH="%s:$PATH"\n' "$install_dir"
        printf '%s <<<\n' "$PATH_MARKER_END"
    } >> "$rc_path"

    log "  ✓ added install-dir PATH export to $rc_path"
}

# --- Main ---

main() {
    log "nimblegate installer"
    log "  install dir:   $NIMBLEGATE_INSTALL_DIR"

    local source
    source="$(resolve_source)"
    log "  source:        ${source#*:}"

    # Create install dir + place binary
    mkdir -p "$NIMBLEGATE_INSTALL_DIR"
    local dest="$NIMBLEGATE_INSTALL_DIR/nimblegate"
    install_binary "$source" "$dest"
    log "  ✓ installed:   $dest"

    # Add install dir to PATH in rc file
    local rc_path
    rc_path="$(detect_rc_file)"
    add_install_path_to_rc "$rc_path" "$NIMBLEGATE_INSTALL_DIR"

    # Prepend install dir to PATH for the rest of THIS script - so the
    # `nimblegate setup` call below resolves to the just-installed binary
    # without requiring the user to source rc first.
    export PATH="$NIMBLEGATE_INSTALL_DIR:$PATH"

    log ""
    log "Running nimblegate setup..."
    log ""

    local setup_args=()
    if [ -n "$NIMBLEGATE_NONINTERACTIVE" ]; then
        setup_args+=("--yes")
    fi

    if ! "$dest" setup "${setup_args[@]}"; then
        err "nimblegate setup failed"
        exit 1
    fi

    log ""
    log "✓ nimblegate install complete"
    log ""
    log "Next:"
    log "  source $rc_path          # reload your shell to pick up PATH"
    log "  nimblegate --help                   # confirm binary is on PATH"
    log "  cd <project> && nimblegate init     # onboard a project"
    log ""
    log "To uninstall:  nimblegate purge"
}

main "$@"
