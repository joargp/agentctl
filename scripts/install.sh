#!/bin/sh
# Install agentctl — tries go install first, falls back to GitHub release binary.
set -e

REPO="joargp/agentctl"
BIN="agentctl"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

# Already installed?
if command -v agentctl >/dev/null 2>&1; then
  echo "agentctl already installed: $(command -v agentctl)"
  exit 0
fi

# Try go install first (always gets latest, no arch lookup needed)
if command -v go >/dev/null 2>&1; then
  echo "Installing via go install..."
  go install github.com/${REPO}@latest
  echo "Installed to $(go env GOPATH)/bin/agentctl"
  exit 0
fi

# Fall back to downloading a pre-built release binary
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64)  ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *)
    echo "Unsupported architecture: $ARCH" >&2
    exit 1
    ;;
esac

case "$OS" in
  darwin|linux) ;;
  *)
    echo "Unsupported OS: $OS (agentctl requires tmux)" >&2
    exit 1
    ;;
esac

VERSION="${VERSION:-$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | sed 's/.*"tag_name": *"\(.*\)".*/\1/')}"
if [ -z "$VERSION" ]; then
  echo "Could not determine latest release version" >&2
  exit 1
fi

URL="https://github.com/${REPO}/releases/download/${VERSION}/${BIN}_${OS}_${ARCH}"
echo "Downloading ${BIN} ${VERSION} for ${OS}/${ARCH}..."
curl -fsSL "$URL" -o "/tmp/${BIN}"
chmod +x "/tmp/${BIN}"

# Install to INSTALL_DIR (may need sudo)
if [ -w "$INSTALL_DIR" ]; then
  mv "/tmp/${BIN}" "${INSTALL_DIR}/${BIN}"
else
  echo "Installing to ${INSTALL_DIR} (sudo required)..."
  sudo mv "/tmp/${BIN}" "${INSTALL_DIR}/${BIN}"
fi

echo "Installed to ${INSTALL_DIR}/${BIN}"
