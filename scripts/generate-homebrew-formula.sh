#!/usr/bin/env bash

# Generate the Homebrew formula for a released version.
#
# Usage: generate-homebrew-formula.sh <tag> <checksums.txt> <output>

set -euo pipefail

usage() {
  cat <<'EOF'
Usage: generate-homebrew-formula.sh <tag> <checksums.txt> <output>

The tag must be a stable semantic version such as v0.1.0. The checksums file
must contain entries for:
  opencode-jobs_darwin_arm64.tar.gz
  opencode-jobs_darwin_amd64.tar.gz
  opencode-jobs_linux_arm64.tar.gz
  opencode-jobs_linux_amd64.tar.gz
EOF
}

if [ "$#" -ne 3 ]; then
  usage >&2
  exit 1
fi

TAG="$1"
CHECKSUMS="$2"
OUTPUT="$3"

if [[ ! "$TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  printf 'tag must be a stable semantic version such as v0.1.0: %s\n' "$TAG" >&2
  exit 1
fi

sha_for() {
  local asset="$1"
  local value
  value="$(awk -v asset="$asset" '$2 == asset || $2 == ("*" asset) { print $1; exit }' "$CHECKSUMS")"
  if [ "${#value}" -ne 64 ] || [[ ! "$value" =~ ^[0-9A-Fa-f]+$ ]]; then
    printf 'missing or invalid checksum for %s\n' "$asset" >&2
    exit 1
  fi
  printf '%s\n' "$value"
}

DARWIN_ARM64="$(sha_for opencode-jobs_darwin_arm64.tar.gz)"
DARWIN_AMD64="$(sha_for opencode-jobs_darwin_amd64.tar.gz)"
LINUX_ARM64="$(sha_for opencode-jobs_linux_arm64.tar.gz)"
LINUX_AMD64="$(sha_for opencode-jobs_linux_amd64.tar.gz)"

URL_BASE="https://github.com/Martins6/opencode-jobs/releases/download/${TAG}"

OUTPUT_DIR="$(dirname "$OUTPUT")"
mkdir -p "$OUTPUT_DIR"
TEMP_OUTPUT="$(mktemp "${OUTPUT_DIR}/.opencode-jobs.rb.XXXXXX")"
trap 'rm -f "$TEMP_OUTPUT"' EXIT

cat > "$TEMP_OUTPUT" <<EOF
class OpencodeJobs < Formula
  desc "Schedule recurring OpenCode agent jobs with launchd, systemd, or cron"
  homepage "https://github.com/Martins6/opencode-jobs"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "${URL_BASE}/opencode-jobs_darwin_arm64.tar.gz"
      sha256 "${DARWIN_ARM64}"
    else
      url "${URL_BASE}/opencode-jobs_darwin_amd64.tar.gz"
      sha256 "${DARWIN_AMD64}"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "${URL_BASE}/opencode-jobs_linux_arm64.tar.gz"
      sha256 "${LINUX_ARM64}"
    else
      url "${URL_BASE}/opencode-jobs_linux_amd64.tar.gz"
      sha256 "${LINUX_AMD64}"
    end
  end

  def install
    bin.install "opencode-jobs"
  end

  test do
    assert_match "opencode-jobs", shell_output("#{bin}/opencode-jobs version")
  end
end
EOF

chmod 0644 "$TEMP_OUTPUT"
mv "$TEMP_OUTPUT" "$OUTPUT"
TEMP_OUTPUT=""
