#!/usr/bin/env bash
# Wire git remotes for private deploy vs public showcase.
set -euo pipefail

PRIVATE_URL="${PRIVATE_URL:-https://github.com/Yojimboshi/wallet-snipe.git}"
SHOWCASE_URL="${SHOWCASE_URL:-https://github.com/Yojimboshi/wallet-intel.git}"

cd "$(git rev-parse --show-toplevel)"

# If origin still points at wallet-intel, rename it to showcase first.
CURRENT_ORIGIN="$(git remote get-url origin 2>/dev/null || true)"
if [[ "$CURRENT_ORIGIN" == *wallet-intel* ]] && ! git remote get-url showcase &>/dev/null; then
  git remote rename origin showcase
fi

if git remote get-url origin &>/dev/null; then
  git remote set-url origin "$PRIVATE_URL"
else
  git remote add origin "$PRIVATE_URL"
fi

if git remote get-url showcase &>/dev/null; then
  git remote set-url showcase "$SHOWCASE_URL"
else
  git remote add showcase "$SHOWCASE_URL"
fi

echo "origin (private deploy): $(git remote get-url origin)"
echo "showcase (public):       $(git remote get-url showcase)"
