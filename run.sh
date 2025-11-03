#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

if [ ! -x ./setup.sh ]; then
  chmod +x ./setup.sh || true
fi

./setup.sh

echo "Building server..."
for bin in  exunreg25; do
  if [ -f "$bin" ]; then
    rm -f "$bin" || true
    echo "Removed old binary: $bin"
  fi
done
export PATH=/usr/local/go/bin:$PATH

if go build -o exunreg25 .; then
  echo "Built exunreg25"
else
  echo "Build failed" >&2
  exit 1
fi
echo "███████╗ ██╗  ██╗ ██╗   ██╗ ███╗   ██╗"
echo "██╔════╝ ██║  ██║ ██║   ██║ ████╗  ██║"
echo "█████╗     ███╔═╝ ██║   ██║ ██╔██╗ ██║"
echo "██╔══╝   ██╔══██║ ██║   ██║ ██║╚██╗██║"
echo "███████╗ ██║  ██║ ╚██████╔╝ ██║ ╚████║"
echo "╚══════╝ ╚═╝  ╚═╝  ╚═════╝  ╚═╝  ╚═══╝"

./exunreg25
