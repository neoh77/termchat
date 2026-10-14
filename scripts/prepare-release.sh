#!/usr/bin/env bash
set -euo pipefail

: "${GITHUB_SHA:?GITHUB_SHA is required}" "${GITHUB_OUTPUT:?GITHUB_OUTPUT is required}"

write_version() {
  printf 'version=%s\n' "$1" >> "$GITHUB_OUTPUT"
}

# Reserve the tag before building so the binary and its release have the same version.
# If another build reserved this version, fetch its tag and try the next patch.
for attempt in {1..10}; do
  git fetch --tags --force origin

  while IFS= read -r tag; do
    if [[ $tag =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] &&
       [[ $(git rev-list -n 1 "$tag") == "$GITHUB_SHA" ]]; then
      write_version "$tag"
      exit 0
    fi
  done < <(git tag --list 'v*')

  tag=$(git tag --list | bash scripts/next-version.sh)
  if git push origin "${GITHUB_SHA}:refs/tags/${tag}"; then
    write_version "$tag"
    exit 0
  fi
done

printf 'Could not reserve a release version after 10 attempts\n' >&2
exit 1
