#!/usr/bin/env bash
# Push a sanitized tree to wallet-intel (no secrets / runtime config).
set -euo pipefail

SHOWCASE_REMOTE="${SHOWCASE_REMOTE:-showcase}"
SHOWCASE_BRANCH="${SHOWCASE_BRANCH:-master}"
SOURCE_BRANCH="${SOURCE_BRANCH:-master}"

cd "$(git rev-parse --show-toplevel)"
ROOT="$PWD"

git fetch "$SHOWCASE_REMOTE" 2>/dev/null || true

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

git archive "$SOURCE_BRANCH" | tar -x -C "$TMP"

# Showcase uses its own .gitignore (separate repo — not the private origin one).
cp "$ROOT/scripts/showcase.gitignore" "$TMP/.gitignore"
mkdir -p "$TMP/config"
cp "$ROOT/scripts/showcase-config-README.md" "$TMP/config/README.md"

# Belt-and-suspenders if older commits still tracked secrets before gitignore.
rm -rf "$TMP/data"
rm -f "$TMP/config/local.json" \
  "$TMP/config/watch.json" \
  "$TMP/config/execution-wallets.json" \
  "$TMP/config/collectors.json" \
  "$TMP/config/"*.local.json \
  "$TMP/intel" "$TMP/intel.exe" "$TMP/"*.exe

cd "$TMP"
git init -q
git checkout -b "$SHOWCASE_BRANCH"

AUTHOR_NAME="$(git -C "$ROOT" log -1 --format='%an' 2>/dev/null || echo wallet-intel)"
AUTHOR_EMAIL="$(git -C "$ROOT" log -1 --format='%ae' 2>/dev/null || echo wallet-intel@local)"
export GIT_AUTHOR_NAME="$AUTHOR_NAME" GIT_COMMITTER_NAME="$AUTHOR_NAME"
export GIT_AUTHOR_EMAIL="$AUTHOR_EMAIL" GIT_COMMITTER_EMAIL="$AUTHOR_EMAIL"

git add -A
git commit -q -m "sync from wallet-snipe"

git remote add showcase "$(git -C "$ROOT" remote get-url "$SHOWCASE_REMOTE")"
git push -f showcase "HEAD:$SHOWCASE_BRANCH"

echo "Pushed sanitized tree to $SHOWCASE_REMOTE ($SHOWCASE_BRANCH)"
