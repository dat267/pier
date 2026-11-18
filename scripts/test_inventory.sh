#!/usr/bin/env bash
# Reports how large the test suite is, per package and overall, and maintains
# the manifest the docs check compares against the tree.
#
# `internal/docscheck` fails when a `*_test.go` file is missing from
# internal/docscheck/testfiles.txt or listed there after it is gone, so a merge
# or deletion has to drop its line and a new file needs a deliberate entry.
# Keeping the manifest exact is what stops the suite from drifting back to one
# file per scenario.
#
# Usage: scripts/test_inventory.sh           report files, tests and lines
#        scripts/test_inventory.sh --write   rewrite the manifest
set -euo pipefail
cd "$(dirname "$0")/.."
manifest=internal/docscheck/testfiles.txt

# Match what docscheck walks: tracked and new files, skipping the ignored
# scratch trees (the pinned ./pi checkout, tmp/, bin/).
test_files() {
	git ls-files --cached --others --exclude-standard '*.go' |
		grep '_test\.go$' | LC_ALL=C sort
}

if [[ ${1:-} == "--write" ]]; then
	{
		echo "# Every *_test.go file in the repository, one path per line, byte-sorted."
		echo "# Internal/docscheck keeps this exact; regenerate with scripts/test_inventory.sh --write."
		test_files
	} >"$manifest"
	echo "wrote $(( $(wc -l <"$manifest") - 2 )) files to $manifest"
	exit 0
fi

printf '%-28s %7s %7s %9s\n' package files tests lines
rows=$(test_files | while read -r file; do
	pkg=${file%/*}
	if [[ $pkg == "$file" ]]; then pkg=.; fi
	tests=$(grep -c '^func Test' "$file" || true)
	printf '%s\t%s\t%s\n' "$pkg" "$tests" "$(wc -l <"$file")"
done)
printf '%s\n' "$rows" | awk -F'\t' '
	{
		files[$1]++
		tests[$1] += $2
		lines[$1] += $3
	}
	END {
		for (pkg in files) printf "%-28s %7d %7d %9d\n", pkg, files[pkg], tests[pkg], lines[pkg]
	}' | sort
printf '%s\n' "$rows" | awk -F'\t' '
	{
		files++
		tests += $2
		lines += $3
	}
	END { printf "%-28s %7d %7d %9d\n", "total", files, tests, lines }'
