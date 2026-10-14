#!/usr/bin/env bash
set -euo pipefail

check() {
  local expected=$1 tags=$2 actual
  actual=$(bash scripts/next-version.sh <<< "$tags")
  if [[ $actual != "$expected" ]]; then
    printf 'Expected %s, got %s for tags: %s\n' "$expected" "$actual" "$tags" >&2
    exit 1
  fi
}

check v0.1.0 ''
check v0.1.0 'build-5dcfe3edbcbccc2af89268abb472188f46666b46'
check v0.1.1 'v0.1.0'
check v0.2.4 $'v0.2.3\nv0.1.9\nv0.2.1'
check v1.0.1 $'v0.99.99\nv1.0.0\nv1.0.0-rc.1\nv01.2.3'

printf 'Version selection tests passed\n'
