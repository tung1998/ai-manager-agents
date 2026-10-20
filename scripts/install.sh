#!/usr/bin/env bash
# Checks Go, Node 22+ and pnpm, installs what is missing into ~/.local
# (no sudo), then builds bin/office and the dashboard.
#   scripts/install.sh          check, install, build
#   scripts/install.sh --run    … then start the office
set -euo pipefail

cd "$(dirname "$0")/.."
PREFIX="${AGENT_OFFICE_TOOLS:-$HOME/.local}"
NODE_MAJOR=22
GO_VERSION="$(awk '/^go /{print $2; exit}' go.mod)"
ADDED_PATHS=()

say() { printf '\033[1;34m==>\033[0m %s\n' "$*"; }
die() { printf '\033[1;31mx\033[0m %s\n' "$*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

case "$(uname -s)" in
  Darwin) OS=darwin ;;
  Linux) OS=linux ;;
  MINGW* | MSYS* | CYGWIN*) die "on Windows run scripts\\install.ps1 in PowerShell (sets up WSL2 and runs this script there)" ;;
  *) die "unsupported OS $(uname -s): install Go $GO_VERSION, Node $NODE_MAJOR and pnpm by hand" ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) GOARCH=amd64 NODEARCH=x64 ;;
  arm64 | aarch64) GOARCH=arm64 NODEARCH=arm64 ;;
  *) die "unsupported CPU $(uname -m)" ;;
esac

have git || die "git is missing (macOS: xcode-select --install, Linux: apt/dnf install git)"
have curl || die "curl is missing"
have tar || die "tar is missing"

add_path() {
  export PATH="$1:$PATH"
  ADDED_PATHS+=("$1")
}

# Tools installed by an earlier run
[ -x "$PREFIX/go/bin/go" ] && add_path "$PREFIX/go/bin"
[ -x "$PREFIX/node/bin/node" ] && add_path "$PREFIX/node/bin"
[ -d "$PREFIX/bin" ] && export PATH="$PREFIX/bin:$PATH"

# --- Go ---------------------------------------------------------------------
# Go >= 1.21 downloads the toolchain go.mod asks for by itself (GOTOOLCHAIN).
GO_OK=0
if have go; then
  minor="$(go env GOVERSION 2>/dev/null | sed -E 's/^go1\.([0-9]+).*/\1/')"
  case "$minor" in '' | *[!0-9]*) ;; *) [ "$minor" -ge 21 ] && GO_OK=1 ;; esac
fi
if [ "$GO_OK" = 1 ]; then
  say "Go: $(go version | awk '{print $3}') (go.mod wants $GO_VERSION, fetched automatically if older)"
else
  say "Go: installing go$GO_VERSION into $PREFIX/go"
  mkdir -p "$PREFIX"
  rm -rf "$PREFIX/go"
  curl -fsSL "https://go.dev/dl/go$GO_VERSION.$OS-$GOARCH.tar.gz" | tar -xz -C "$PREFIX"
  add_path "$PREFIX/go/bin"
  go version
fi

# --- Node -------------------------------------------------------------------
NODE_OK=0
if have node; then
  major="$(node -p 'process.versions.node.split(".")[0]' 2>/dev/null || echo 0)"
  [ "$major" -ge "$NODE_MAJOR" ] && NODE_OK=1
fi
if [ "$NODE_OK" = 1 ]; then
  say "Node: $(node -v)"
else
  say "Node: installing Node $NODE_MAJOR into $PREFIX/node"
  base="https://nodejs.org/dist/latest-v$NODE_MAJOR.x"
  file="$(curl -fsSL "$base/SHASUMS256.txt" | awk -v s="-$OS-$NODEARCH.tar.gz" 'index($2, s) {print $2; exit}')"
  [ -n "$file" ] || die "no Node $NODE_MAJOR build for $OS-$NODEARCH"
  mkdir -p "$PREFIX"
  rm -rf "$PREFIX/node" "$PREFIX/${file%.tar.gz}"
  curl -fsSL "$base/$file" | tar -xz -C "$PREFIX"
  mv "$PREFIX/${file%.tar.gz}" "$PREFIX/node"
  add_path "$PREFIX/node/bin"
  node -v
fi

# --- pnpm -------------------------------------------------------------------
if have pnpm; then
  say "pnpm: $(pnpm -v)"
else
  say "pnpm: installing into $PREFIX/bin"
  npm install -g pnpm --prefix "$PREFIX" --silent
  add_path "$PREFIX/bin"
  pnpm -v
fi

# --- Build ------------------------------------------------------------------
say "Building bin/office"
go build -o bin/office ./cmd/office
say "Building the dashboard"
(cd dashboard && pnpm install --frozen-lockfile && pnpm build)

# Keep the tools on PATH for new shells
if [ ${#ADDED_PATHS[@]} -gt 0 ]; then
  case "${SHELL:-}" in
    */zsh) rc="$HOME/.zshrc" ;;
    */bash) [ "$OS" = darwin ] && rc="$HOME/.bash_profile" || rc="$HOME/.bashrc" ;;
    *) rc="$HOME/.profile" ;;
  esac
  for p in "${ADDED_PATHS[@]}"; do
    line="export PATH=\"$p:\$PATH\""
    grep -qxF "$line" "$rc" 2>/dev/null || { echo "$line" >>"$rc"; say "Added $p to PATH in $rc"; }
  done
fi

say "Done. Start the office: ./bin/office run  →  http://localhost:2704 (admin / admin)"
if [ "${1:-}" = "--run" ]; then
  exec ./bin/office run
fi
