#!/usr/bin/env bash
set -euo pipefail

: "${GITHUB_SHA:?GITHUB_SHA is required}" "${GH_TOKEN:?GH_TOKEN is required}"
asset=dist/termchat

publish() {
  local tag=$1
  if gh release view "$tag" >/dev/null 2>&1; then
    gh release upload "$tag" "$asset" --clobber
  else
    gh release create "$tag" "$asset" \
      --verify-tag \
      --title "termchat $tag" \
      --generate-notes
  fi
}

# Reserve the tag on the remote before publishing. If another build reserved
# the same version, fetch its tag and try the next patch number.
for attempt in {1..10}; do
  git fetch --tags --force origin

  while IFS= read -r tag; do
    if [[ $tag =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] &&
       [[ $(git rev-list -n 1 "$tag") == "$GITHUB_SHA" ]]; then
      publish "$tag"
      exit 0
    fi
  done < <(git tag --list 'v*')

  tag=$(git tag --list | bash scripts/next-version.sh)
  if git push origin "${GITHUB_SHA}:refs/tags/${tag}"; then
    publish "$tag"
    exit 0
  fi
done

printf 'Could not reserve a release version after 10 attempts\n' >&2
exit 1
