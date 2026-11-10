#!/usr/bin/env bash
# One native amd64 CI cell. The integrated runner owns service cleanup.
set -euo pipefail
umask 077

[[ $# == 2 ]] || { echo 'Expected Ubuntu version and fresh|foreign-upgrade mode' >&2; exit 2; }
ubuntu=$1
mode=$2
case "$ubuntu" in
  24.04|26.04) ;;
  *) echo 'Expected Ubuntu 24.04 or 26.04' >&2; exit 2 ;;
esac
case "$mode" in
  fresh) flags=(--fresh) ;;
  foreign-upgrade) flags=(--foreign --upgrade) ;;
  *) echo 'Expected fresh or foreign-upgrade mode' >&2; exit 2 ;;
esac
: "${ACCEPTANCE_LOG_DIR:?Set ACCEPTANCE_LOG_DIR to a new directory}"
[[ ! -e "$ACCEPTANCE_LOG_DIR" && ! -L "$ACCEPTANCE_LOG_DIR" ]] || {
  echo 'ACCEPTANCE_LOG_DIR must not exist' >&2; exit 2;
}

framework=$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd -P)
build_info=$(mktemp -d)
trap 'rm -rf "$build_info"' EXIT

if [[ "$mode" == foreign-upgrade ]]; then
  baseline=$(cd "$framework/tests/fixtures/deploy-acceptance" &&
    "${GO:-go}" mod download &&
    "${GO:-go}" list -m -f '{{.Dir}}' github.com/weiloon1234/Foundry-Go)
  [[ -n "$baseline" && -f "$baseline/tools/deploy/protocol.json" ]] || {
    echo 'Fixture pin did not resolve to a published deploy tools bundle' >&2; exit 1;
  }
  flags+=(--tools-root "$baseline/tools/deploy")
fi

python3 -B "$framework/tools/deploy/tests/build_acceptance.py" --architecture amd64 \
  --archive-file "$build_info/archive.txt"
archive=$(cat "$build_info/archive.txt")
bash "$framework/tools/deploy/tests/run_acceptance.sh" --ubuntu "$ubuntu" --platform linux/amd64 \
  --archive "$archive" --framework-root "$framework" "${flags[@]}"
