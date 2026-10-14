#!/usr/bin/env bash
set -euo pipefail

# Read existing tags on stdin. Ignore legacy tags and SemVer prereleases.
major=-1
minor=0
patch=0
while IFS= read -r tag; do
  if [[ $tag =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
    candidate_major=${BASH_REMATCH[1]}
    candidate_minor=${BASH_REMATCH[2]}
    candidate_patch=${BASH_REMATCH[3]}
    if (( candidate_major > major ||
          (candidate_major == major && candidate_minor > minor) ||
          (candidate_major == major && candidate_minor == minor && candidate_patch > patch) )); then
      major=$candidate_major
      minor=$candidate_minor
      patch=$candidate_patch
    fi
  fi
done

if (( major < 0 )); then
  printf 'v0.1.0\n'
else
  printf 'v%d.%d.%d\n' "$major" "$minor" "$((patch + 1))"
fi
