# The Go port of pi (module github.com/dat267/pier).
#
# The CLI is the module root (a thin main.go) over the cmd package, so
# `go build .` and `go install .` both work — `install` wraps the latter.
#
# `just --list` shows the recipes; the default is `build`.

set shell := ["bash", "-euo", "pipefail", "-c"]

module := "github.com/dat267/pier"
bin := "bin/pier"

# The CLI prints this for --version, so a local build names the commit it came from
# (`git describe`; the port carries no tags, so that is the short SHA) instead of the
# source default 0.0.0, which cannot answer "which build am I running?". VERSION
# overrides it as before: `just VERSION=1.2.3 install`. A checkout without git (a
# tarball) yields an empty version and falls back to the source default, and the
# release workflow passes its own (release.yml stamps the tag or the SHA).
VERSION := env_var_or_default("VERSION", `git describe --tags --always --dirty 2>/dev/null || true`)

# Pure Go: no cgo anywhere in the port, and the release workflow builds the same
# way (GOOS=android CGO_ENABLED=0).
ldflags := "-trimpath -ldflags \"-s -w" + (if VERSION == "" { "" } else { " -X " + module + "/coding.Version=" + VERSION }) + "\""

# Build bin/pier.
build:
	@mkdir -p bin
	CGO_ENABLED=0 go build {{ldflags}} -o {{bin}} .
	@echo "built {{bin}}"

# The Unix variant uses the global bash shell; the Windows variant is
# PowerShell, so install works there without a POSIX shell on PATH.
#
# Install the CLI into GOBIN (or GOPATH/bin) and warn if that is not on PATH.
[unix]
install:
	#!/usr/bin/env bash
	set -euo pipefail
	CGO_ENABLED=0 go install {{ldflags}} .
	dir="$(go env GOBIN)"
	if [[ -z "$dir" ]]; then dir="$(go env GOPATH)/bin"; fi
	echo "installed $dir/pier"
	case ":$PATH:" in *":$dir:"*) ;; *) echo "note: $dir is not on PATH" ;; esac

[windows]
install:
	#!powershell
	$ErrorActionPreference = "Stop"
	$env:CGO_ENABLED = "0"
	$ld = "-s -w"
	$version = '{{VERSION}}'
	if ($version) { $ld = "$ld -X github.com/dat267/pier/coding.Version=$version" }
	$goArgs = @("install", "-trimpath", "-ldflags", $ld, ".")
	& go @goArgs
	$dir = (& go env GOBIN | Out-String).Trim()
	if (-not $dir) { $dir = Join-Path ((& go env GOPATH | Out-String).Trim()) "bin" }
	Write-Output ("installed " + (Join-Path $dir "pier.exe"))
	$target = $dir.TrimEnd('\', '/')
	$onPath = $false
	foreach ($entry in ($env:PATH -split ';')) {
		if ($entry.Trim().TrimEnd('\', '/') -ieq $target) { $onPath = $true; break }
	}
	if (-not $onPath) { Write-Output ("note: " + $dir + " is not on PATH") }

# Run the test suite (the local gate; CI adds -race -count=2).
test:
	go test -count=1 ./...

# Run the test suite under the race detector (not available on android/arm64 — CI runs this).
test-race:
	go test -race ./...

# Report formatting differences.
fmt:
	#!/usr/bin/env bash
	set -euo pipefail
	out="$(gofmt -l .)"
	if [[ -n "$out" ]]; then echo "$out"; exit 1; fi

# Run go vet.
vet:
	go vet ./...

# What CI runs, minus -race (the test and cross-build jobs).
check: fmt vet test

# Cross-build and cross-vet every OS the port targets (mirrors the CI matrix).
cross:
	#!/usr/bin/env bash
	set -euo pipefail
	for pair in windows/amd64 windows/arm64 darwin/arm64 darwin/amd64 linux/arm64 \
		android/arm64; do
		echo "$pair"
		CGO_ENABLED=0 GOOS="${pair%/*}" GOARCH="${pair#*/}" go build ./...
		CGO_ENABLED=0 GOOS="${pair%/*}" GOARCH="${pair#*/}" go vet ./...
	done

# Remove build output.
clean:
	rm -rf bin

# The reference checkout is only as useful as the tags it holds: a diff against "the
# latest pi" reads them, and a checkout at the wrong tag silently answers the wrong
# question. The pin itself is in docs/PORTING.md; these read and check that state.
# Fetch upstream tags into ./pi without moving it off the pinned tag.
reference-fetch:
	git -C pi fetch --tags --quiet origin
	@echo "fetched upstream tags into pi"

# Reads local state only, so it works offline; `just reference-fetch` refreshes the
# tags it can see. It fails when the checkout is at another tag, because that is the
# state in which an "against upstream" review quietly becomes wrong.
# Check that ./pi is at the pin in docs/PORTING.md, and report upstream's drift.
reference:
	#!/usr/bin/env bash
	set -euo pipefail

	if [[ ! -d pi/.git ]]; then
		echo "reference: no ./pi checkout; clone it at the pin:" >&2
		echo "      git clone https://github.com/earendil-works/pi pi" >&2
		echo "      git -C pi checkout --detach v1.0.2   # the pin, see docs/PORTING.md" >&2
		exit 1
	fi

	# "Reference pin: `cd32f772` (Release v1.0.2), read from the `./pi` checkout."
	pin_commit="$(sed -n 's/^Reference pin: `\([0-9a-f]*\)`.*/\1/p' docs/PORTING.md | head -1)"
	pin_tag="$(sed -n 's/^Reference pin: `[0-9a-f]*` (Release \([^)]*\)).*/\1/p' docs/PORTING.md | head -1)"
	if [[ -z "$pin_commit" || -z "$pin_tag" ]]; then
		echo "reference: no pin in docs/PORTING.md (expected a 'Reference pin:' line)" >&2
		exit 1
	fi

	head_commit="$(git -C pi rev-parse HEAD)"
	at="$(git -C pi describe --tags --exact-match HEAD 2>/dev/null || git -C pi describe --tags --always HEAD)"
	echo "pin:      $pin_tag ($pin_commit), from docs/PORTING.md"
	echo "checkout: $at ($(git -C pi rev-parse --short HEAD))"

	latest="$(git -C pi tag --sort=-v:refname | head -1)"
	if [[ -n "$latest" && "$latest" != "$pin_tag" ]]; then
		behind="$(git -C pi rev-list --count "$pin_tag..$latest" 2>/dev/null || echo '?')"
		echo "upstream: $latest is $behind commit(s) past the pin; not ported (see the known drift in docs/PORTING.md)"
	fi

	if [[ "$head_commit" == "$pin_commit"* ]]; then
		echo "reference: ok, ./pi is at the pin"
		exit 0
	fi
	echo "reference: ./pi is at $at, not the pin $pin_tag" >&2
	echo "      check it out with: git -C pi checkout --detach $pin_tag" >&2
	exit 1

# Fast-forward when the history is linear, a merge commit when it is not. The tree
# must be clean, and a conflict aborts the merge and reports the files instead of
# leaving a half-finished one. Nothing is pushed, so the run ends by saying what is
# still to send.
# Sync the current branch with its upstream (fast-forward or merge).
sync:
	#!/usr/bin/env bash
	set -euo pipefail

	if ! git rev-parse --git-dir >/dev/null 2>&1; then
		echo "sync: not a git repository" >&2
		exit 1
	fi

	branch="$(git symbolic-ref -q --short HEAD || true)"
	if [[ -z "$branch" ]]; then
		echo "sync: HEAD is detached; check out a branch first" >&2
		exit 1
	fi

	if [[ -f "$(git rev-parse --git-dir)/MERGE_HEAD" ]]; then
		echo "sync: a merge is already in progress; finish it or run: git merge --abort" >&2
		exit 1
	fi

	if ! upstream="$(git rev-parse --abbrev-ref --symbolic-full-name '@{u}' 2>/dev/null)"; then
		echo "sync: $branch has no upstream; set one with:" >&2
		echo "      git branch --set-upstream-to=origin/$branch $branch" >&2
		exit 1
	fi

	if [[ -n "$(git status --porcelain --untracked-files=no)" ]]; then
		echo "sync: $branch has uncommitted changes; commit or stash them first" >&2
		echo "      (a conflict would otherwise have to be unwound around them)" >&2
		exit 1
	fi

	remote="${upstream%%/*}"
	if ! git fetch --prune --quiet "$remote"; then
		echo "sync: could not fetch from $remote; $branch is unchanged" >&2
		exit 1
	fi

	before="$(git rev-parse HEAD)"
	if ! git merge --ff --no-edit "$upstream"; then
		if [[ -f "$(git rev-parse --git-dir)/MERGE_HEAD" ]]; then
			echo "sync: conflicts in:" >&2
			git diff --name-only --diff-filter=U | sed 's/^/      /' >&2
			git merge --abort
			echo "sync: merge aborted; $branch is unchanged" >&2
			echo "      resolve them in the merge itself with: git merge $upstream" >&2
		else
			echo "sync: merge failed; $branch is unchanged" >&2
		fi
		exit 1
	fi

	after="$(git rev-parse HEAD)"
	if [[ "$before" == "$after" ]]; then
		echo "already up to date with $upstream"
	else
		echo "merged $upstream into $branch ($(git rev-parse --short "$before")..$(git rev-parse --short "$after"))"
	fi

	ahead="$(git rev-list --count "$upstream..HEAD")"
	if [[ "$ahead" != "0" ]]; then
		echo "$ahead commit(s) not pushed yet: git push"
	fi
