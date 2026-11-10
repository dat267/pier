#!/usr/bin/env bash
# Runs every top-level test of each package on its own, in a fresh process.
# A test that passes in the suite but fails alone depends on state an earlier
# test left behind (the active theme, a package-level default), which a
# shuffled run only catches when the dependent test happens to run first.
#
# Usage: scripts/test_isolated.sh [packages...]   (default: ./...)
# COUNT=2 runs each test twice in its process, so one that leaves global state
# behind (a sync.Once, a package default) fails its own second run.
set -euo pipefail

jobs=${JOBS:-8}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
failed=0
mapfile -t packages < <(go list "${@:-./...}")
for package in "${packages[@]}"; do
	dir=$(go list -f '{{.Dir}}' "$package")
	binary="$work/$(basename "$dir").test"
	go test -c -o "$binary" "$package" >/dev/null
	[[ -x $binary ]] || continue # no test files
	failures="$work/failures"
	: >"$failures"
	(cd "$dir" && "$binary" -test.list '^Test' | grep '^Test' || true) |
		xargs -r -P "$jobs" -I{} env COUNT="${COUNT:-1}" bash -c \
			'cd "$1" && PIER_LEAKCHECK=off "$2" -test.run "^$3\$" -test.short -test.count=${COUNT:-1} >/dev/null 2>&1 || echo "$3" >>"$4"' \
			_ "$dir" "$binary" {} "$failures"
	if [[ -s $failures ]]; then
		failed=1
		sort "$failures" | sed "s|^|$package |"
	fi
	rm -f "$binary"
done
exit "$failed"
