#!/usr/bin/env bash
set -euo pipefail

: "${RELEASE_TAG:?RELEASE_TAG is required}" "${GH_TOKEN:?GH_TOKEN is required}"
asset=dist/termchat

if gh release view "$RELEASE_TAG" >/dev/null 2>&1; then
  gh release upload "$RELEASE_TAG" "$asset" --clobber
else
  gh release create "$RELEASE_TAG" "$asset" \
    --verify-tag \
    --title "termchat $RELEASE_TAG" \
    --generate-notes
fi
