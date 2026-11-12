#!/usr/bin/env bash

# opencode-jobs installer for macOS and Linux.
#
#   curl -fsSL https://raw.githubusercontent.com/Martins6/opencode-jobs/main/install.sh | bash
#
# Environment:
#   OPENCODE_JOBS_VERSION      Release tag (default: latest stable)
#   OPENCODE_JOBS_INSTALL_DIR  Install directory (default: ~/.local/bin)

set -euo pipefail

REPO="Martins6/opencode-jobs"
BINARY="opencode-jobs"
VERSION="${OPENCODE_JOBS_VERSION:-latest}"
PREFIX="${OPENCODE_JOBS_INSTALL_DIR:-$HOME/.local/bin}"
MAIN_BUILD=0

if [ -t 1 ]; then
  RED=$'\033[0;31m'
  GREEN=$'\033[0;32m'
  YELLOW=$'\033[1;33m'
  NC=$'\033[0m'
else
  RED=""
  GREEN=""
  YELLOW=""
  NC=""
fi

info() { printf '%s\n' "$*"; }
success() { printf '%s%s%s\n' "$GREEN" "$*" "$NC"; }
warn() { printf '%s%s%s\n' "$YELLOW" "$*" "$NC"; }
error() { printf '%s%s%s\n' "$RED" "$*" "$NC" >&2; }

usage() {
  cat <<'EOF'
Usage: install.sh [options]

Options:
  --version <tag>   Install a specific release, for example v0.1.0
  --main            Install the latest rolling build from main
  --prefix <dir>    Install to this directory (default: ~/.local/bin)
  -h, --help        Show this help

Environment:
  OPENCODE_JOBS_VERSION      Same as --version
  OPENCODE_JOBS_INSTALL_DIR  Same as --prefix
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --version)
      [ "$#" -ge 2 ] || { error "--version requires a value"; exit 1; }
      VERSION="$2"
      shift 2
      ;;
    --main)
      MAIN_BUILD=1
      shift
      ;;
    --prefix)
      [ "$#" -ge 2 ] || { error "--prefix requires a value"; exit 1; }
      PREFIX="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      error "unknown option: $1"
      usage >&2
      exit 1
      ;;
  esac
done

detect_platform() {
  case "$(uname -s)" in
    Darwin) OS="darwin" ;;
    Linux) OS="linux" ;;
    *)
      error "unsupported operating system: $(uname -s). opencode-jobs supports macOS and Linux."
      exit 1
      ;;
  esac
  case "$(uname -m)" in
    x86_64|amd64) ARCH="amd64" ;;
    arm64|aarch64) ARCH="arm64" ;;
    *)
      error "unsupported architecture: $(uname -m)"
      exit 1
      ;;
  esac
}

download() {
  url="$1"
  destination="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL --retry 3 "$url" -o "$destination"
  elif command -v wget >/dev/null 2>&1; then
    wget -q "$url" -O "$destination"
  else
    error "need curl or wget to download a release"
    return 1
  fi
}

sha256() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    error "need sha256sum or shasum to verify the download"
    return 1
  fi
}

detect_platform
ASSET="${BINARY}_${OS}_${ARCH}.tar.gz"

if [ "$MAIN_BUILD" -eq 1 ]; then
  BASE_URL="https://github.com/${REPO}/releases/download/latest"
  DISPLAY_VERSION="latest main build"
elif [ "$VERSION" = "latest" ]; then
  BASE_URL="https://github.com/${REPO}/releases/latest/download"
  DISPLAY_VERSION="latest release"
else
  case "$VERSION" in
    v*) TAG="$VERSION" ;;
    *) TAG="v$VERSION" ;;
  esac
  BASE_URL="https://github.com/${REPO}/releases/download/${TAG}"
  DISPLAY_VERSION="$TAG"
fi

TMP_DIR="$(mktemp -d "${TMPDIR:-/tmp}/opencode-jobs-install.XXXXXX")"
trap 'rm -rf "$TMP_DIR"' EXIT

info "Installing ${BINARY} (${DISPLAY_VERSION}) for ${OS}/${ARCH}..."
download "${BASE_URL}/${ASSET}" "${TMP_DIR}/${ASSET}"
download "${BASE_URL}/checksums.txt" "${TMP_DIR}/checksums.txt"

EXPECTED="$(awk -v asset="$ASSET" '$2 == asset || $2 == ("*" asset) { print $1; exit }' "${TMP_DIR}/checksums.txt")"
if [ -z "$EXPECTED" ]; then
  error "checksums.txt does not contain ${ASSET}"
  exit 1
fi
ACTUAL="$(sha256 "${TMP_DIR}/${ASSET}")"
if [ "$EXPECTED" != "$ACTUAL" ]; then
  error "checksum verification failed for ${ASSET}"
  error "expected: ${EXPECTED}"
  error "actual:   ${ACTUAL}"
  exit 1
fi

tar -xzf "${TMP_DIR}/${ASSET}" -C "${TMP_DIR}"
if [ ! -f "${TMP_DIR}/${BINARY}" ]; then
  error "archive did not contain ${BINARY}"
  exit 1
fi

if ! mkdir -p "$PREFIX" 2>/dev/null || [ ! -w "$PREFIX" ]; then
  error "cannot write to ${PREFIX}"
  error "choose another directory with --prefix, or create it with the right permissions"
  exit 1
fi

install -m 0755 "${TMP_DIR}/${BINARY}" "${PREFIX}/${BINARY}"
success "Installed ${PREFIX}/${BINARY}"

case ":${PATH:-}:" in
  *":${PREFIX}:"*) ;;
  *)
    warn ""
    warn "Add ${PREFIX} to your PATH:"
    warn "  export PATH=\"${PREFIX}:\$PATH\""
    ;;
esac

info ""
"${PREFIX}/${BINARY}" version
info ""
info "Next: npx skills add Martins6/opencode-jobs --skill opencode-jobs -g -a opencode -y"
