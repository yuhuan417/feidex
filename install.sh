#!/usr/bin/env bash
#
# Feidex one-line installer.
#
#   curl -fsSL https://raw.githubusercontent.com/yuhuan417/feidex/main/install.sh | bash
#
# Downloads the release binary for your OS/arch, verifies its SHA-256 against
# the release's sha256sums.txt, installs it to a bin directory on your PATH,
# and prints the next steps. No build toolchain required.
#
# Options (pass after `| bash -s --`):
#   --version <vX.Y.Z>   Install a specific version (default: latest release)
#   --dev                Install the latest dev build (dev-latest prerelease)
#   --bin-dir <dir>      Install directory (default: ~/.local/bin)
#   --repo <owner/name>  GitHub repo to install from (default: yuhuan417/feidex)
#   --no-modify-path     Do not offer to add the bin dir to your shell profile
#   -h, --help           Show this help
#
# Environment overrides: FEIDEX_VERSION, FEIDEX_REPO, FEIDEX_BIN_DIR
set -euo pipefail

REPO="${FEIDEX_REPO:-yuhuan417/feidex}"
VERSION="${FEIDEX_VERSION:-}"
BIN_DIR="${FEIDEX_BIN_DIR:-}"
USE_DEV=0
MODIFY_PATH=1

# ---- pretty output (TTY-aware) ------------------------------------------------
if [ -t 1 ] && [ -z "${NO_COLOR:-}" ]; then
  C_RESET="$(printf '\033[0m')"; C_BOLD="$(printf '\033[1m')"
  C_RED="$(printf '\033[31m')"; C_GREEN="$(printf '\033[32m')"; C_YELLOW="$(printf '\033[33m')"
else
  C_RESET=""; C_BOLD=""; C_RED=""; C_GREEN=""; C_YELLOW=""
fi
info()  { printf '%s==>%s %s\n' "${C_BOLD}" "${C_RESET}" "$*"; }
ok()    { printf '%s✓%s %s\n'  "${C_GREEN}" "${C_RESET}" "$*"; }
warn()  { printf '%s!%s %s\n'  "${C_YELLOW}" "${C_RESET}" "$*" >&2; }
die()   { printf '%serror:%s %s\n' "${C_RED}" "${C_RESET}" "$*" >&2; exit 1; }

usage() {
  cat <<'EOF'
Feidex one-line installer.

  curl -fsSL https://raw.githubusercontent.com/yuhuan417/feidex/main/install.sh | bash

Downloads the release binary for your OS/arch, verifies its SHA-256 against the
release's sha256sums.txt, installs it to a bin directory on your PATH, and
prints the next steps. No build toolchain required.

Options (pass after `| bash -s --`):
  --version <vX.Y.Z>   Install a specific version (default: latest release)
  --dev                Install the latest dev build (dev-latest prerelease)
  --bin-dir <dir>      Install directory (default: ~/.local/bin)
  --repo <owner/name>  GitHub repo to install from (default: yuhuan417/feidex)
  --no-modify-path     Do not offer to add the bin dir to your shell profile
  -h, --help           Show this help

Environment overrides: FEIDEX_VERSION, FEIDEX_REPO, FEIDEX_BIN_DIR

Examples:
  curl -fsSL .../install.sh | bash
  curl -fsSL .../install.sh | bash -s -- --dev
  curl -fsSL .../install.sh | bash -s -- --version v0.222.0 --bin-dir /usr/local/bin
EOF
}

# ---- args ---------------------------------------------------------------------
while [ $# -gt 0 ]; do
  case "$1" in
    --version) VERSION="${2:-}"; shift 2 ;;
    --dev) USE_DEV=1; shift ;;
    --bin-dir) BIN_DIR="${2:-}"; shift 2 ;;
    --repo) REPO="${2:-}"; shift 2 ;;
    --no-modify-path) MODIFY_PATH=0; shift ;;
    -h|--help) usage; exit 0 ;;
    *) die "unknown option: $1 (use --help)" ;;
  esac
done

# ---- downloader ---------------------------------------------------------------
if command -v curl >/dev/null 2>&1; then
  DL="curl"
elif command -v wget >/dev/null 2>&1; then
  DL="wget"
else
  die "need curl or wget installed"
fi
fetch_to() { # url dest
  if [ "$DL" = curl ]; then curl -fsSL --proto '=https' --tlsv1.2 -o "$2" "$1"
  else wget -q -O "$2" "$1"; fi
}
fetch_stdout() { # url
  if [ "$DL" = curl ]; then curl -fsSL --proto '=https' --tlsv1.2 "$1"
  else wget -qO- "$1"; fi
}

# ---- platform detection -------------------------------------------------------
os="$(uname -s)"; arch="$(uname -m)"
case "$os" in
  Linux)  goos="linux" ;;
  Darwin) goos="darwin" ;;
  *) die "unsupported OS: $os (build from source instead)" ;;
esac
case "$arch" in
  x86_64|amd64) goarch="amd64" ;;
  arm64|aarch64) goarch="arm64" ;;
  *) die "unsupported architecture: $arch" ;;
esac
# Asset naming: linux arm64 ships as 'aarch64'; everything else uses goarch.
if [ "$goos" = linux ] && [ "$goarch" = arm64 ]; then
  asset="feidex-linux-aarch64"
else
  asset="feidex-${goos}-${goarch}"
fi

# ---- resolve version ----------------------------------------------------------
resolve_latest() {
  if [ "$DL" = curl ]; then
    # Follow the /releases/latest redirect; the final URL ends in the tag.
    local eff; eff="$(curl -fsSLI -o /dev/null -w '%{url_effective}' \
      "https://github.com/${REPO}/releases/latest" 2>/dev/null || true)"
    [ -n "$eff" ] && printf '%s\n' "${eff##*/}"
  fi
  if [ -z "${eff:-}" ]; then
    # Fallback: parse the releases API (no jq).
    fetch_stdout "https://api.github.com/repos/${REPO}/releases/latest" \
      | grep -m1 '"tag_name"' | sed -E 's/.*"tag_name":[[:space:]]*"([^"]+)".*/\1/'
  fi
}
if [ "$USE_DEV" -eq 1 ]; then
  TAG="dev-latest"
elif [ -n "$VERSION" ]; then
  TAG="$VERSION"
else
  info "Resolving latest release of ${REPO}…"
  TAG="$(resolve_latest)"
  [ -n "$TAG" ] || die "could not resolve latest release tag"
fi

base="https://github.com/${REPO}/releases/download/${TAG}"
info "Installing ${C_BOLD}feidex ${TAG}${C_RESET} (${goos}/${goarch}) — asset ${asset}"

# ---- download + verify --------------------------------------------------------
tmp="$(mktemp -d "${TMPDIR:-/tmp}/feidex-install.XXXXXX")"
trap 'rm -rf "$tmp"' EXIT
bin_tmp="${tmp}/feidex"

info "Downloading binary…"
fetch_to "${base}/${asset}" "$bin_tmp" \
  || die "download failed: ${base}/${asset} (does tag ${TAG} exist?)"

info "Verifying SHA-256…"
if fetch_to "${base}/sha256sums.txt" "${tmp}/sha256sums.txt" 2>/dev/null; then
  expected="$(awk -v a="$asset" '$2 ~ ("(^|/)" a "$") {print $1; exit}' "${tmp}/sha256sums.txt")"
  [ -n "$expected" ] || die "no checksum entry for ${asset} in sha256sums.txt"
  if command -v sha256sum >/dev/null 2>&1; then
    actual="$(sha256sum "$bin_tmp" | awk '{print $1}')"
  elif command -v shasum >/dev/null 2>&1; then
    actual="$(shasum -a 256 "$bin_tmp" | awk '{print $1}')"
  else
    actual=""; warn "no sha256 tool found; skipping verification"
  fi
  if [ -n "$actual" ]; then
    [ "$actual" = "$expected" ] || die "checksum mismatch for ${asset}
  expected ${expected}
  actual   ${actual}"
    ok "checksum verified"
  fi
else
  warn "sha256sums.txt not available for ${TAG}; skipping verification"
fi
chmod +x "$bin_tmp"

# ---- install ------------------------------------------------------------------
if [ -z "$BIN_DIR" ]; then
  BIN_DIR="${HOME}/.local/bin"
fi
mkdir -p "$BIN_DIR" || die "cannot create ${BIN_DIR}"
dest="${BIN_DIR}/feidex"
# Atomic replace (handles the binary being on PATH already).
mv -f "$bin_tmp" "${dest}.new" && mv -f "${dest}.new" "$dest"
ok "Installed to ${C_BOLD}${dest}${C_RESET}"

# Sanity check.
if "$dest" version >/dev/null 2>&1; then
  ok "$("$dest" version 2>/dev/null | head -1)"
fi

# ---- PATH handling ------------------------------------------------------------
case ":${PATH}:" in
  *":${BIN_DIR}:"*) on_path=1 ;;
  *) on_path=0 ;;
esac
if [ "$on_path" -eq 0 ]; then
  if [ "$MODIFY_PATH" -eq 1 ]; then
    profile=""
    case "${SHELL:-}" in
      */zsh) profile="${HOME}/.zshrc" ;;
      */bash) [ -f "${HOME}/.bashrc" ] && profile="${HOME}/.bashrc" || profile="${HOME}/.bash_profile" ;;
      *) profile="${HOME}/.profile" ;;
    esac
    line="export PATH=\"${BIN_DIR}:\$PATH\""
    if [ -n "$profile" ] && ! grep -qsF "$line" "$profile" 2>/dev/null; then
      printf '\n# Added by feidex install.sh\n%s\n' "$line" >> "$profile"
      ok "Added ${BIN_DIR} to PATH in ${profile} (restart your shell or 'source ${profile}')"
    else
      warn "${BIN_DIR} is not on your PATH; add: ${line}"
    fi
  else
    warn "${BIN_DIR} is not on your PATH; add: export PATH=\"${BIN_DIR}:\$PATH\""
  fi
fi

# ---- next steps ---------------------------------------------------------------
cat <<EOF

${C_GREEN}${C_BOLD}feidex ${TAG} is installed.${C_RESET}

Next steps:
  1. Bind a Feishu/Lark app and write config.toml:
       feidex feishu setup                      # 飞书（国内版）
       feidex feishu bind --app ID:SECRET --domain lark   # Lark（国际版）
  2. Start it:
       feidex serve --config config.toml
  3. (Linux) run it in the background with auto-upgrade:
       feidex daemon install

Docs: https://github.com/${REPO}#readme
EOF
