#!/usr/bin/env bash
# Run on the server: pull private repo, build, restart wallet-intel.
set -euo pipefail

APP_DIR="${APP_DIR:-$HOME/wallet-snipe}"
SERVICE="${SERVICE:-wallet-intel}"
BRANCH="${BRANCH:-master}"
REPO_URL="${REPO_URL:-https://github.com/Yojimboshi/wallet-snipe.git}"

if [[ ! -d "$APP_DIR/.git" ]]; then
  echo "Cloning private repo into $APP_DIR ..."
  git clone --branch "$BRANCH" "$REPO_URL" "$APP_DIR"
fi

cd "$APP_DIR"
git fetch origin
git checkout "$BRANCH"
git pull --ff-only origin "$BRANCH"

mkdir -p data

if ! command -v go &>/dev/null; then
  echo "Go not installed. Install Go 1.22+ or upload a pre-built ./intel binary."
  exit 1
fi

echo "Building intel ..."
go build -o intel ./cmd/intel
chmod +x intel

if command -v systemctl &>/dev/null && systemctl list-unit-files "$SERVICE.service" &>/dev/null; then
  echo "Restarting systemd service $SERVICE ..."
  sudo systemctl restart "$SERVICE"
  sudo systemctl --no-pager status "$SERVICE"
else
  echo "Built ./intel — no systemd unit '$SERVICE' found."
  echo "Start manually: cd $APP_DIR && ./intel"
fi

echo "Deploy done."
