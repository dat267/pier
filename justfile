# The Go port of pi (module github.com/dat267/pier).
#
# The CLI is the module root (a thin main.go) over the cmd package, so
# `go build .` and `go install .` both work — `install` wraps the latter.
#
# `just --list` shows the recipes; the default is `build`.

set shell := ["bash", "-euo", "pipefail", "-c"]
# Recipe parameters arrive as $1, $2, ... (without this just interpolates {{param}}
# and passes nothing).
set positional-arguments

module := "github.com/dat267/pier"
bin := "bin/pier"

# The CLI prints this for --version, so a local build names the commit it came from
# (`git describe`; the port carries no tags, so that is the short SHA) instead of the
# source default 0.0.0, which cannot answer "which build am I running?". VERSION
# overrides it as before: `just VERSION=1.2.3 install`. A checkout without git (a
# tarball) yields an empty version and falls back to the source default, and the
# release workflow passes its own (android-release.yml stamps the tag or the SHA).
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

# Run the test suite.
test:
	go test ./...

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
	for pair in windows/amd64 darwin/arm64 darwin/amd64 linux/arm64 \
		freebsd/amd64 openbsd/amd64 netbsd/amd64 dragonfly/amd64 solaris/amd64 aix/ppc64; do
		echo "$pair"
		CGO_ENABLED=0 GOOS="${pair%/*}" GOARCH="${pair#*/}" go build ./...
		CGO_ENABLED=0 GOOS="${pair%/*}" GOARCH="${pair#*/}" go vet ./...
	done

# Remove build output.
clean:
	rm -rf bin

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

# Bump the newest tag and push it: pushing the tag is the release — the
# android-release workflow builds the binaries and attaches them to a GitHub
# release, which is what `pier update` installs. The tag message lists the commits
# since the last tag, and the workflow publishes it as the release body.
# Bump and push a release tag (major, minor or patch).
release part:
	#!/usr/bin/env bash
	set -euo pipefail

	# `set positional-arguments` (top of this file) is what puts the parameter in
	# $1; without it just interpolates {{part}} into the body and passes nothing.
	bump="${1:-}"
	case "$bump" in
	major|minor|patch) ;;
	*) echo "release: expected major, minor or patch, got '${bump}'" >&2; exit 1 ;;
	esac

	if ! git rev-parse --git-dir >/dev/null 2>&1; then
		echo "release: not a git repository" >&2
		exit 1
	fi
	branch="$(git symbolic-ref -q --short HEAD || true)"
	if [[ -z "$branch" ]]; then
		echo "release: HEAD is detached; check out the branch to release" >&2
		exit 1
	fi
	if [[ -n "$(git status --porcelain)" ]]; then
		echo "release: the working tree has uncommitted changes; commit or stash them first" >&2
		exit 1
	fi

	remote="${REMOTE:-origin}"
	git fetch --quiet --tags "$remote"
	if ! git rev-parse --verify --quiet "$remote/$branch" >/dev/null; then
		echo "release: $remote/$branch does not exist; push the branch first" >&2
		exit 1
	fi
	if ! git merge-base --is-ancestor HEAD "$remote/$branch"; then
		echo "release: HEAD is not pushed to $remote/$branch; push it first" >&2
		exit 1
	fi

	# The newest version tag; a repository with none starts from v0.0.0.
	latest="$(git describe --tags --abbrev=0 --match 'v[0-9]*' 2>/dev/null || echo v0.0.0)"
	version="${latest#v}"
	IFS=. read -r major minor patch <<<"$version"
	minor="${minor:-0}"
	patch="${patch:-0}"
	if ! [[ "$major" =~ ^[0-9]+$ && "$minor" =~ ^[0-9]+$ && "$patch" =~ ^[0-9]+$ ]]; then
		echo "release: cannot read $latest as major.minor.patch" >&2
		exit 1
	fi
	case "$bump" in
	major) major=$((major + 1)); minor=0; patch=0 ;;
	minor) minor=$((minor + 1)); patch=0 ;;
	patch) patch=$((patch + 1)) ;;
	esac
	next="v$major.$minor.$patch"
	if git rev-parse --verify --quiet "refs/tags/$next" >/dev/null; then
		echo "release: $next already exists" >&2
		exit 1
	fi

	# The tag message becomes the release body, so it lists what is in the release.
	if git rev-parse --verify --quiet "refs/tags/$latest" >/dev/null; then
		range="$latest..HEAD"
	else
		range="HEAD"
	fi
	notes="$(git log --oneline --no-decorate $range | sed 's/^[0-9a-f]* //')"
	message="pier $next"
	if [[ -n "$notes" ]]; then
		message="$message"$'\n\n'"$notes"
	fi

	git tag -a "$next" -m "$message"
	git push "$remote" "$next"
	echo "released $next (was $latest)"

	# The release page, derived from the remote so a fork prints its own.
	slug="$(git remote get-url "$remote")"
	slug="${slug%.git}"
	slug="${slug#git@github.com:}"
	slug="${slug#https://github.com/}"
	slug="${slug#ssh://git@github.com/}"
	echo "binaries: https://github.com/$slug/releases/tag/$next"
