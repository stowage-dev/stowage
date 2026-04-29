#!/bin/sh
# stowage downloader for macOS, Linux, and WSL.
#
# Downloads the platform-appropriate stowage binary into the current
# directory, verifies its SHA256 checksum, and runs it. Nothing is added
# to PATH or any system location.
#
#   curl -fsSL https://stowage.dev/install.sh | sh
#   curl -fsSL https://stowage.dev/install.sh | sh -s -- serve --config my.yaml
#
# Environment overrides:
#   STOWAGE_VERSION       Tag to fetch (default: latest)
#   STOWAGE_REPO          GitHub owner/name (default: stowage-dev/stowage)
#   STOWAGE_RELEASE_BASE  Full base URL for binary downloads. Overrides REPO/VERSION.
#   STOWAGE_NO_RUN        If set to 1, download and verify but do not exec.
set -eu

REPO="${STOWAGE_REPO:-stowage-dev/stowage}"
VERSION="${STOWAGE_VERSION:-latest}"

if [ -n "${STOWAGE_RELEASE_BASE:-}" ]; then
  BASE="${STOWAGE_RELEASE_BASE}"
elif [ "${VERSION}" = "latest" ]; then
  BASE="https://github.com/${REPO}/releases/latest/download"
else
  BASE="https://github.com/${REPO}/releases/download/${VERSION}"
fi

err() { printf 'stowage-install: %s\n' "$*" >&2; exit 1; }
log() { printf '==> %s\n' "$*"; }

uname_s="$(uname -s 2>/dev/null || echo unknown)"
uname_m="$(uname -m 2>/dev/null || echo unknown)"

case "${uname_s}" in
  Linux*)  os=linux ;;
  Darwin*) os=darwin ;;
  MINGW*|MSYS*|CYGWIN*) err "use install.ps1 on Windows (PowerShell) or install.cmd (CMD)" ;;
  *) err "unsupported OS: ${uname_s}" ;;
esac

case "${uname_m}" in
  x86_64|amd64)  arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) err "unsupported architecture: ${uname_m}" ;;
esac

asset="stowage-${os}-${arch}"
url="${BASE}/${asset}"
sums_url="${BASE}/SHA256SUMS"

if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL -o "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -qO "$2" "$1"; }
else
  err "need either curl or wget"
fi

if command -v sha256sum >/dev/null 2>&1; then
  sha256() { sha256sum "$1" | awk '{print $1}'; }
elif command -v shasum >/dev/null 2>&1; then
  sha256() { shasum -a 256 "$1" | awk '{print $1}'; }
else
  err "need either sha256sum or shasum to verify the download"
fi

tmp="$(mktemp -d 2>/dev/null || mktemp -d -t stowage)"
cleanup() { [ -n "${tmp:-}" ] && rm -rf "${tmp}"; }
trap cleanup EXIT INT TERM

log "downloading ${asset} from ${BASE}"
fetch "${url}"      "${tmp}/${asset}"
fetch "${sums_url}" "${tmp}/SHA256SUMS"

expected="$(awk -v f="${asset}" '{ name=$2; sub(/^\*/, "", name); if (name == f) { print $1; exit } }' "${tmp}/SHA256SUMS")"
[ -n "${expected}" ] || err "${asset} not present in SHA256SUMS at ${sums_url}"
actual="$(sha256 "${tmp}/${asset}")"
if [ "${expected}" != "${actual}" ]; then
  err "checksum mismatch for ${asset} (expected ${expected}, got ${actual})"
fi
log "checksum ok"

target="./stowage"
chmod +x "${tmp}/${asset}"
mv "${tmp}/${asset}" "${target}"
log "downloaded to ${target}"

if [ "${STOWAGE_NO_RUN:-0}" = "1" ]; then
  log "STOWAGE_NO_RUN=1 set; skipping run"
  exit 0
fi

log "running: ${target} $*"
exec "${target}" "$@"
