package coding

import (
	"context"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// Port of utils/version-check.ts, including the semver `valid`/`compare`
// subset the release check needs (the Go port has no semver dependency).

// PortRepositoryURL is this module's repository: releases live under it, and
// `pier update` builds its asset URLs from it.
const PortRepositoryURL = "https://github.com/dat267/pier"

// PortReleaseURL is where the newest release is announced: GitHub's
// /releases/latest redirect, whose Location is .../releases/tag/<tag>. It is
// deliberately not the REST API: an unauthenticated API client gets 60 requests an
// hour per IP and then a 403, and a mobile IP shares that budget with everyone
// behind it, which is enough for `pier update` to fail on a device that has not
// used the API at all. The redirect is not rate-limited and needs no credentials.
//
// Upstream asks pi's release feed (utils/version-check.ts), whose versions are
// pi's, not this port's; the Go module proxy is not consulted either, so nothing
// here depends on the port being published to proxy.golang.org.
const PortReleaseURL = PortRepositoryURL + "/releases/latest"

// PortReleasesPage is the human-facing release list, used when a release payload
// carries no page URL of its own.
const PortReleasesPage = PortRepositoryURL + "/releases"

// DefaultVersionCheckTimeoutMS bounds the version request.
const DefaultVersionCheckTimeoutMS int64 = 10_000

// LatestRelease is the published release info. Upstream's feed carries notes as
// well; a GitHub release carries the page URL of those notes instead.
type LatestRelease struct {
	Version string
	URL     string
}

// ComparePackageVersions compares two semver strings; ok is false when either
// side is not valid semver.
func ComparePackageVersions(leftVersion, rightVersion string) (int, bool) {
	left, ok := parseSemver(strings.TrimSpace(leftVersion))
	if !ok {
		return 0, false
	}
	right, ok := parseSemver(strings.TrimSpace(rightVersion))
	if !ok {
		return 0, false
	}
	return left.compare(right), true
}

// IsNewerPackageVersion reports whether candidate is newer than current. When
// either side is not valid semver the raw strings are compared instead.
func IsNewerPackageVersion(candidateVersion, currentVersion string) bool {
	if comparison, ok := ComparePackageVersions(candidateVersion, currentVersion); ok {
		return comparison > 0
	}
	return strings.TrimSpace(candidateVersion) != strings.TrimSpace(currentVersion)
}

// semver is a parsed semantic version.
type semver struct {
	major, minor, patch int
	prerelease          []string
	build               string
}

// parseSemver parses a semver string (the npm `semver.valid` subset: strict
// x.y.z with optional -prerelease and +build).
func parseSemver(value string) (semver, bool) {
	if value == "" {
		return semver{}, false
	}
	main := value
	// npm's semver accepts one leading "v" and strips it (`valid("v1.2.3")` is
	// "1.2.3"), which matters because Go module versions keep the tag's prefix.
	if strings.HasPrefix(main, "v") {
		main = main[1:]
	}
	if index := strings.Index(main, "+"); index >= 0 {
		build := main[index+1:]
		if build == "" || !validBuildIdentifiers(build) {
			return semver{}, false
		}
		main = main[:index]
	}
	var prerelease []string
	if index := strings.Index(main, "-"); index >= 0 {
		pre := main[index+1:]
		if pre == "" || !validPrereleaseIdentifiers(pre) {
			return semver{}, false
		}
		prerelease = strings.Split(pre, ".")
		main = main[:index]
	}
	parts := strings.Split(main, ".")
	if len(parts) != 3 {
		return semver{}, false
	}
	numbers := make([]int, 3)
	for index, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return semver{}, false
		}
		number, err := strconv.Atoi(part)
		if err != nil || number < 0 {
			return semver{}, false
		}
		numbers[index] = number
	}
	return semver{major: numbers[0], minor: numbers[1], patch: numbers[2], prerelease: prerelease}, true
}

func validPrereleaseIdentifiers(value string) bool {
	for _, identifier := range strings.Split(value, ".") {
		if identifier == "" {
			return false
		}
		numeric := true
		for _, character := range identifier {
			if character < '0' || character > '9' {
				numeric = false
				break
			}
		}
		if numeric {
			// Numeric identifiers must not have leading zeroes.
			if len(identifier) > 1 && identifier[0] == '0' {
				return false
			}
			continue
		}
		for _, character := range identifier {
			if !isSemverIdentifierCharacter(character) {
				return false
			}
		}
	}
	return true
}

func validBuildIdentifiers(value string) bool {
	for _, identifier := range strings.Split(value, ".") {
		if identifier == "" {
			return false
		}
		for _, character := range identifier {
			if !isSemverIdentifierCharacter(character) {
				return false
			}
		}
	}
	return true
}

func isSemverIdentifierCharacter(character rune) bool {
	switch {
	case character >= '0' && character <= '9':
		return true
	case character >= 'a' && character <= 'z':
		return true
	case character >= 'A' && character <= 'Z':
		return true
	case character == '-':
		return true
	}
	return false
}

// compare returns -1, 0, or 1 per semver precedence.
func (v semver) compare(other semver) int {
	if v.major != other.major {
		return compareInts(v.major, other.major)
	}
	if v.minor != other.minor {
		return compareInts(v.minor, other.minor)
	}
	if v.patch != other.patch {
		return compareInts(v.patch, other.patch)
	}
	// A version without a prerelease outranks one with a prerelease.
	if len(v.prerelease) == 0 && len(other.prerelease) == 0 {
		return 0
	}
	if len(v.prerelease) == 0 {
		return 1
	}
	if len(other.prerelease) == 0 {
		return -1
	}
	length := len(v.prerelease)
	if len(other.prerelease) < length {
		length = len(other.prerelease)
	}
	for index := 0; index < length; index++ {
		left, right := v.prerelease[index], other.prerelease[index]
		leftNumber, leftErr := strconv.Atoi(left)
		rightNumber, rightErr := strconv.Atoi(right)
		switch {
		case leftErr == nil && rightErr == nil:
			if leftNumber != rightNumber {
				return compareInts(leftNumber, rightNumber)
			}
		case leftErr == nil:
			return -1 // numeric identifiers rank lower than alphanumeric
		case rightErr == nil:
			return 1
		default:
			if left != right {
				if left < right {
					return -1
				}
				return 1
			}
		}
	}
	return compareInts(len(v.prerelease), len(other.prerelease))
}

func compareInts(left, right int) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

// getLatestPortReleaseFrom resolves the newest release from a /releases/latest
// redirect, whose Location names the tag. A server that answers without redirecting,
// or whose Location names no tag, yields nothing rather than an error.
func getLatestPortReleaseFrom(ctx context.Context, url, currentVersion string, client *http.Client) (*LatestRelease, error) {
	if client == nil {
		client = http.DefaultClient
	}
	// The redirect *is* the answer, so it is not followed: its target is the release
	// page, whose HTML names nothing the tag does not.
	noRedirect := *client
	noRedirect.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", PiUserAgent(currentVersion))
	timeoutMS := DefaultVersionCheckTimeoutMS
	response, err := FetchWithRetry(ctx, request, &noRedirect, FetchRetryOptions{TimeoutMS: &timeoutMS})
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 300 || response.StatusCode >= 400 {
		return nil, nil
	}
	location, err := response.Location()
	if err != nil {
		return nil, nil
	}
	tag := releaseTagFromLocation(location.Path)
	if tag == "" {
		return nil, nil
	}
	return &LatestRelease{Version: tag, URL: location.String()}, nil
}

// releaseTagFromLocation reads the tag out of a .../releases/tag/<tag> path. This
// repository's releases are v-prefixed, so anything else (the release list, a path
// with no tag) is not a release.
func releaseTagFromLocation(path string) string {
	tag := path[strings.LastIndex(path, "/")+1:]
	if len(tag) < 2 || tag[0] != 'v' {
		return ""
	}
	for _, r := range tag[1:] {
		isDigit := r >= '0' && r <= '9'
		isLetter := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		if !isDigit && !isLetter && r != '.' && r != '-' {
			return ""
		}
	}
	return tag
}

// checkForLatestPortReleaseFrom is the check over an explicit endpoint, so a
// test can point it at a local server.
func checkForLatestPortReleaseFrom(ctx context.Context, url, currentVersion string, client *http.Client) *LatestRelease {
	release, err := getLatestPortReleaseFrom(ctx, url, currentVersion, client)
	if err != nil || release == nil {
		return nil
	}
	if !isComparableVersion(currentVersion) {
		return nil
	}
	if IsNewerPackageVersion(release.Version, currentVersion) {
		return release
	}
	return nil
}

// isComparableVersion reports whether a running version can be ordered against a
// release tag. Two cases cannot, and both must stay silent rather than notify:
// a version that is not semver at all (a commit hash, which `just install`
// stamps and which IsNewerPackageVersion would then compare as a string — the
// inequality would read as "newer" forever), and the unstamped default, which
// means "no version stamped" and sorts below every real release.
func isComparableVersion(version string) bool {
	trimmed := strings.TrimSpace(version)
	if trimmed == "" || trimmed == "0.0.0" {
		return false
	}
	_, ok := ComparePackageVersions(trimmed, trimmed)
	return ok
}

// CheckForLatestPortRelease returns a newer release of this module, or nil
// (errors are swallowed like upstream).
func CheckForLatestPortRelease(ctx context.Context, currentVersion string) *LatestRelease {
	if os.Getenv("PI_SKIP_VERSION_CHECK") != "" {
		return nil
	}
	return checkForLatestPortReleaseFrom(ctx, PortReleaseURL, currentVersion, http.DefaultClient)
}
