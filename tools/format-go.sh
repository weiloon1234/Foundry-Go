#!/bin/sh
# Format repository Go sources, including consumer/compile-fail fixtures, while
# excluding private caches, dependency trees and tool-owned workspace state.
set -eu
mode=$1
formatter=$2
case "$mode" in write|check) ;; *) printf '%s\n' 'Expected write or check' >&2; exit 2 ;; esac
files=$(mktemp)
output=$(mktemp)
trap 'rm -f "$files" "$output"' EXIT HUP INT TERM
find . -type d \( -name .git -o -name .cache -o -name vendor -o -name node_modules -o -name bin -o -name .agents -o -name .codex \) -prune -o -type f -name '*.go' -print0 > "$files"
if [ "$mode" = write ]; then
    xargs -0 "$formatter" -w < "$files"
else
    xargs -0 "$formatter" -l < "$files" > "$output"
    if [ -s "$output" ]; then
        printf '%s\n' 'Go files need formatting:'
        cat "$output"
        exit 1
    fi
fi
