package coding

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// `pier update` (D175) installs this module's own release asset. Upstream has the
// same command, but it runs the package manager and updates its packages and
// extensions with it (package-manager-cli.ts); extensions and the package manager
// are out of scope for the port (D41), so the update is the release binary the
// release workflow attaches, verified against the checksum it attaches beside it.
//
// The release is found through the /releases/latest redirect and its assets are
// fetched by name, both deliberately without the REST API: an unauthenticated API
// client gets 60 requests an hour per IP and then a 403, and a mobile IP shares that
// budget with everyone behind it — which is how `pier update` came to fail with a
// 403 on a device that had not used the API at all.

// DefaultUpdateTimeoutMS bounds a whole update, download included: the asset is
// ~16 MB, and a phone on a slow link needs longer than the 10s the startup release
// check allows itself.
const DefaultUpdateTimeoutMS int64 = 5 * 60 * 1000

// errAssetNotFound marks a release asset that is not there. A platform with no build
// is detected through it, so it is not a failure to report on its own.
var errAssetNotFound = errors.New("asset not found")

// UpdateCommandError is a user-facing update failure.
type UpdateCommandError struct{ Message string }

func (e *UpdateCommandError) Error() string { return e.Message }

// updateAssetName is the release asset for a platform, as the release workflow
// names it (pier-android-arm64).
func updateAssetName(goos, goarch string) string {
	return "pier-" + goos + "-" + goarch
}

// latestReleaseURL is the redirect that names the newest release.
func latestReleaseURL(repoURL string) string { return repoURL + "/releases/latest" }

// releaseAssetURL is the direct download of one asset of one release.
func releaseAssetURL(repoURL, tag, asset string) string {
	return repoURL + "/releases/download/" + tag + "/" + asset
}

// UpdateOptions are the updater's seams: the repository releases live in, which
// client to use, which binary to replace, and which platform's asset to pick. A test
// fills them in so it never touches the network or the running executable.
type UpdateOptions struct {
	RepoURL string
	Client  *http.Client
	Target  string
	GOOS    string
	GOARCH  string

	// rename replaces a file, injectable so a test can exercise the fallback a
	// Windows self-update needs (see replaceBinary).
	rename func(oldpath, newpath string) error
}

func (o *UpdateOptions) resolve() {
	if o.RepoURL == "" {
		o.RepoURL = PortRepositoryURL
	}
	if o.Client == nil {
		o.Client = http.DefaultClient
	}
	if o.GOOS == "" {
		o.GOOS = runtime.GOOS
	}
	if o.GOARCH == "" {
		o.GOARCH = runtime.GOARCH
	}
	if o.rename == nil {
		o.rename = os.Rename
	}
}

// updateAssetCandidates are the asset names to try for a platform, in order. The
// release workflow appends .exe on Windows, so that name is the expected one there
// but the bare name is still worth trying.
func updateAssetCandidates(goos, goarch string) []string {
	name := updateAssetName(goos, goarch)
	if goos == "windows" {
		return []string{name, name + ".exe"}
	}
	return []string{name}
}

// RunUpdate installs the newest release asset over options.Target and returns the
// version it installed. An empty version with a nil error means the target was
// already at the newest release, so nothing was downloaded.
func RunUpdate(ctx context.Context, options UpdateOptions) (string, error) {
	options.resolve()

	release, err := getLatestPortReleaseFrom(ctx, latestReleaseURL(options.RepoURL), Version, options.Client)
	if err != nil {
		return "", &UpdateCommandError{Message: fmt.Sprintf("the newest release could not be read: %v", err)}
	}
	if release == nil {
		return "", &UpdateCommandError{Message: "no releases are published for this module yet"}
	}
	// An explicit update installs even when the running version cannot be ordered
	// against the tag (a commit hash): the user asked for the release. Only a version
	// that is genuinely at or past the tag is a no-op.
	if !IsNewerPackageVersion(release.Version, Version) {
		return "", nil
	}

	// A self-update without verification is how a truncated download becomes a broken
	// binary, and the workflow always attaches the checksum — so the checksum's
	// presence is also how a platform without a build is detected.
	candidates := updateAssetCandidates(options.GOOS, options.GOARCH)
	assetName, wantSum := "", ""
	for _, candidate := range candidates {
		sum, err := fetchChecksum(ctx, options.Client, releaseAssetURL(options.RepoURL, release.Version, candidate+".sha256"), candidate+".sha256")
		if err != nil {
			if errors.Is(err, errAssetNotFound) {
				continue
			}
			return "", err
		}
		assetName, wantSum = candidate, sum
		break
	}
	if assetName == "" {
		return "", &UpdateCommandError{Message: fmt.Sprintf("release %s has no %s asset", release.Version, candidates[len(candidates)-1])}
	}

	if options.Target == "" {
		return "", &UpdateCommandError{Message: "no target binary to replace"}
	}
	target := options.Target
	// A leftover from a Windows self-update (the running image could not be removed)
	// is cleared on a later run.
	_ = os.Remove(target + updateQuarantineSuffix)

	// The download lands next to the target so the replacement is a rename within one
	// filesystem.
	temp, err := os.CreateTemp(filepath.Dir(target), ".pier-update-*")
	if err != nil {
		return "", &UpdateCommandError{Message: fmt.Sprintf("cannot write next to %s: %v", target, err)}
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()

	gotSum, err := downloadTo(ctx, options.Client, releaseAssetURL(options.RepoURL, release.Version, assetName), assetName, temp)
	closeErr := temp.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	if !strings.EqualFold(gotSum, wantSum) {
		return "", &UpdateCommandError{Message: fmt.Sprintf(
			"the downloaded %s does not match its checksum (want %s, got %s)", assetName, wantSum, gotSum)}
	}
	// A release asset is a blob: it does not keep the executable bit.
	if err := os.Chmod(tempPath, 0o755); err != nil {
		return "", err
	}
	if err := replaceBinary(options.rename, target, tempPath); err != nil {
		return "", &UpdateCommandError{Message: fmt.Sprintf("could not replace %s: %v", target, err)}
	}
	return release.Version, nil
}

// updateQuarantineSuffix names the sidelined binary a Windows self-update leaves
// behind; upstream calls this its self-update quarantine.
const updateQuarantineSuffix = ".old"

// replaceBinary puts the downloaded file in place. A direct rename is atomic and
// works while the binary runs on Unix. On Windows the running image cannot be
// overwritten but can be moved aside, so the fallback renames it out of the way
// first and leaves it for a later run to delete.
func replaceBinary(rename func(string, string) error, target, temp string) error {
	if err := rename(temp, target); err == nil {
		return nil
	}
	quarantine := target + updateQuarantineSuffix
	_ = os.Remove(quarantine)
	if err := rename(target, quarantine); err != nil {
		return err
	}
	if err := rename(temp, target); err != nil {
		_ = rename(quarantine, target)
		return err
	}
	// Fails while it is still the running image; the next run clears it.
	_ = os.Remove(quarantine)
	return nil
}

// fetchChecksum reads a sha256 file and returns its digest ("<hex>  <name>").
func fetchChecksum(ctx context.Context, client *http.Client, url, name string) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", PiUserAgent(Version))
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	switch {
	case response.StatusCode == http.StatusNotFound:
		return "", fmt.Errorf("%s: %w", name, errAssetNotFound)
	case response.StatusCode < 200 || response.StatusCode >= 300:
		return "", &UpdateCommandError{Message: fmt.Sprintf("%s could not be read (HTTP %d)", name, response.StatusCode)}
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4096))
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(raw))
	if len(fields) == 0 || len(fields[0]) != sha256.Size*2 {
		return "", &UpdateCommandError{Message: fmt.Sprintf("%s is not a sha256 digest", name)}
	}
	if _, err := hex.DecodeString(fields[0]); err != nil {
		return "", &UpdateCommandError{Message: fmt.Sprintf("%s is not a sha256 digest", name)}
	}
	return fields[0], nil
}

// downloadTo streams a URL into out and returns the sha256 of what it wrote.
func downloadTo(ctx context.Context, client *http.Client, url, name string, out io.Writer) (string, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", PiUserAgent(Version))
	response, err := client.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	switch {
	case response.StatusCode == http.StatusNotFound:
		return "", &UpdateCommandError{Message: fmt.Sprintf("release has no %s asset", name)}
	case response.StatusCode < 200 || response.StatusCode >= 300:
		return "", &UpdateCommandError{Message: fmt.Sprintf("the download of %s failed (HTTP %d)", name, response.StatusCode)}
	}
	digest := sha256.New()
	if _, err := io.Copy(io.MultiWriter(out, digest), response.Body); err != nil {
		return "", err
	}
	return hex.EncodeToString(digest.Sum(nil)), nil
}

// executableTarget is the binary to replace: the running one with symlinks
// resolved, so an update through a symlinked PATH entry replaces the real file.
func executableTarget() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved, nil
	}
	return path, nil
}

// UpdateCommandUsage renders `pier update`'s help.
func UpdateCommandUsage(appName string) string {
	return fmt.Sprintf(`Usage:
  %[1]s update

Installs the newest release binary for this platform, verifies it against the
release's checksum, and replaces the running binary. Restart %[1]s afterwards.`, appName)
}

// RunUpdateCommand handles a `pier update` invocation. handled is false when the
// first argument is not "update".
func RunUpdateCommand(appName string, args []string, stdout, stderr io.Writer) (bool, int) {
	return runUpdateCommand(appName, args, stdout, stderr, UpdateOptions{})
}

// runUpdateCommand is the command over explicit options, so a test can point it at a
// local release server and a temp file instead of this binary.
func runUpdateCommand(appName string, args []string, stdout, stderr io.Writer, options UpdateOptions) (bool, int) {
	if len(args) == 0 || args[0] != "update" {
		return false, 0
	}
	if len(args) > 1 {
		switch args[1] {
		case "--help", "-h", "help":
			fmt.Fprint(stdout, UpdateCommandUsage(appName))
			return true, 0
		}
	}
	if options.Target == "" {
		target, err := executableTarget()
		if err != nil {
			fmt.Fprintf(stderr, "Error: %v\n", err)
			return true, 1
		}
		options.Target = target
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(DefaultUpdateTimeoutMS)*time.Millisecond)
	defer cancel()

	version, err := RunUpdate(ctx, options)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return true, 1
	}
	if version == "" {
		fmt.Fprintf(stdout, "already up to date (%s)\n", Version)
		return true, 0
	}
	fmt.Fprintf(stdout, "updated to %s; restart %s to use it\n", version, appName)
	return true, 0
}
