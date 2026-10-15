#!/bin/sh
set -eu

echo 'Portable tests only. Full transaction/rollback and UI suites run on macOS CI.'
# Each binary includes Go's testing runner and all fixtures use disposable paths.
found=0
for suite in /tests/*.test; do
    [ -f "$suite" ] || continue
    found=1
    echo "Running ${suite##*/}"
    "$suite" -test.v -test.timeout=3m "$@"
done
[ "$found" -eq 1 ] || { echo 'No compiled test suites found.' >&2; exit 1; }
