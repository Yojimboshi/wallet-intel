#!/usr/bin/env bash
# Push current branch to private origin (wallet-snipe).
set -euo pipefail

BRANCH="${BRANCH:-master}"
REMOTE="${REMOTE:-origin}"

cd "$(git rev-parse --show-toplevel)"

echo "Pushing to $REMOTE ($BRANCH) ..."
git push "$REMOTE" "$BRANCH"
echo "Done. On server: cd ~/wallet-snipe && ./scripts/deploy.sh"
