package coding

// Port of packages/coding-agent/src/utils/clipboard-image.ts: reading an image
// from the system clipboard. Upstream prefers a native (Bun FFI) clipboard and
// falls back to platform commands; the Go port has no native clipboard, so the
// command paths are the whole implementation (D106). Two port-local command
// substitutes stand in for the native backend on Windows (PowerShell
// Get-Clipboard) and macOS (pngpaste); both are best-effort and report no
// image when the helper is unavailable.

import (
	"context"
	"encoding/base64"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// ClipboardImage is image bytes read from the clipboard.
type ClipboardImage struct {
	Bytes    []byte
	MimeType string
}

const (
	clipboardImageListTimeoutMS       = 1000
	clipboardImagePowerShellTimeoutMS = 5000
)

var supportedImageMimeTypes = []string{"image/png", "image/jpeg", "image/webp", "image/gif"}

func baseImageMimeType(mimeType string) string {
	base, _, _ := strings.Cut(mimeType, ";")
	return strings.ToLower(strings.TrimSpace(base))
}

// ExtensionForImageMimeType returns the file extension for a supported image
// MIME type, or "" when unknown.
func ExtensionForImageMimeType(mimeType string) string {
	switch baseImageMimeType(mimeType) {
	case "image/png":
		return "png"
	case "image/jpeg":
		return "jpg"
	case "image/webp":
		return "webp"
	case "image/gif":
		return "gif"
	}
	return ""
}

func isSupportedImageMimeType(mimeType string) bool {
	base := baseImageMimeType(mimeType)
	for _, supported := range supportedImageMimeTypes {
		if supported == base {
			return true
		}
	}
	return false
}

// selectPreferredImageMimeType picks the first supported type in preference
// order, else any image/* type, like upstream.
func selectPreferredImageMimeType(mimeTypes []string) string {
	type candidate struct {
		raw  string
		base string
	}
	var candidates []candidate
	for _, mimeType := range mimeTypes {
		trimmed := strings.TrimSpace(mimeType)
		if trimmed == "" {
			continue
		}
		candidates = append(candidates, candidate{trimmed, baseImageMimeType(trimmed)})
	}
	for _, preferred := range supportedImageMimeTypes {
		for _, entry := range candidates {
			if entry.base == preferred {
				return entry.raw
			}
		}
	}
	for _, entry := range candidates {
		if strings.HasPrefix(entry.base, "image/") {
			return entry.raw
		}
	}
	return ""
}

// IsWaylandSession reports whether the environment is a Wayland session.
func IsWaylandSession(getenv func(string) string) bool {
	return getenv("WAYLAND_DISPLAY") != "" || getenv("XDG_SESSION_TYPE") == "wayland"
}

// IsWSL reports whether the process runs under Windows Subsystem for Linux.
func IsWSL(getenv func(string) string, readFile func(string) ([]byte, error)) bool {
	if getenv("WSL_DISTRO_NAME") != "" || getenv("WSLENV") != "" {
		return true
	}
	if readFile == nil {
		readFile = os.ReadFile
	}
	release, err := readFile("/proc/version")
	if err != nil {
		return false
	}
	lower := strings.ToLower(string(release))
	return strings.Contains(lower, "microsoft") || strings.Contains(lower, "wsl")
}

// clipboardImageRunner runs one clipboard helper; nil means the command failed.
type clipboardImageRunner func(ctx context.Context, command string, args []string, timeoutMS int) []byte

// ReadClipboardImageContext reads an image from the system clipboard, or
// returns nil when there is no image. Errors are reported as nil, like
// upstream's null.
func ReadClipboardImageContext(ctx context.Context) (*ClipboardImage, error) {
	runner := func(ctx context.Context, command string, args []string, timeoutMS int) []byte {
		return runClipboardCommand(ctx, command, args, clipboardCommandOptions{timeoutMS: timeoutMS})
	}
	return readClipboardImageWith(ctx, runtime.GOOS, os.Getenv, runner, os.ReadFile)
}

func readClipboardImageWith(ctx context.Context, goos string, getenv func(string) string, run clipboardImageRunner, readFile func(string) ([]byte, error)) (*ClipboardImage, error) {
	if getenv("TERMUX_VERSION") != "" {
		return nil, nil
	}
	var image *ClipboardImage
	failed := false
	switch goos {
	case "linux":
		wsl := IsWSL(getenv, readFile)
		if IsWaylandSession(getenv) || wsl {
			image, failed = readClipboardImageViaWlPaste(ctx, run)
		}
		if failed {
			image, failed = readClipboardImageViaXclip(ctx, run)
		}
		// An empty Wayland clipboard is not proof Windows has no image; the
		// WSL PowerShell read only runs when nothing was found.
		if image == nil && wsl {
			if viaPowerShell, powerShellFailed := readClipboardImageViaPowerShell(ctx, run, true); !powerShellFailed {
				image = viaPowerShell
			}
		}
	case "windows":
		image, _ = readClipboardImageViaPowerShell(ctx, run, false)
	case "darwin":
		image, _ = readClipboardImageViaPngpaste(ctx, run)
	}
	if image == nil {
		return nil, nil
	}
	if !isSupportedImageMimeType(image.MimeType) {
		if converted, ok := encodeBytesAsPNG(image.Bytes, image.MimeType); ok {
			return &ClipboardImage{Bytes: converted, MimeType: "image/png"}, nil
		}
		return nil, nil
	}
	return image, nil
}

func readClipboardImageViaWlPaste(ctx context.Context, run clipboardImageRunner) (*ClipboardImage, bool) {
	list := run(ctx, "wl-paste", []string{"--list-types"}, clipboardImageListTimeoutMS)
	if list == nil {
		return nil, true
	}
	selected := selectPreferredImageMimeType(strings.Split(strings.ReplaceAll(string(list), "\r\n", "\n"), "\n"))
	if selected == "" {
		return nil, false
	}
	data := run(ctx, "wl-paste", []string{"--type", selected, "--no-newline"}, 0)
	if data == nil {
		return nil, true
	}
	if len(data) == 0 {
		return nil, false
	}
	return &ClipboardImage{Bytes: data, MimeType: baseImageMimeType(selected)}, false
}

func readClipboardImageViaXclip(ctx context.Context, run clipboardImageRunner) (*ClipboardImage, bool) {
	targets := run(ctx, "xclip", []string{"-selection", "clipboard", "-t", "TARGETS", "-o"}, clipboardImageListTimeoutMS)
	if targets == nil {
		return nil, true
	}
	selected := selectPreferredImageMimeType(strings.Split(strings.ReplaceAll(string(targets), "\r\n", "\n"), "\n"))
	if selected == "" {
		return nil, false
	}
	data := run(ctx, "xclip", []string{"-selection", "clipboard", "-t", selected, "-o"}, 0)
	if data == nil {
		return nil, true
	}
	if len(data) == 0 {
		return nil, false
	}
	return &ClipboardImage{Bytes: data, MimeType: baseImageMimeType(selected)}, false
}

// readClipboardImageViaPowerShell reads the Windows clipboard through
// PowerShell, saving a PNG to a temp file. With wsl set the temp path is
// translated through wslpath for the Windows executable.
func readClipboardImageViaPowerShell(ctx context.Context, run clipboardImageRunner, wsl bool) (*ClipboardImage, bool) {
	tempFile := filepath.Join(os.TempDir(), AppName+"-clip-"+UUIDv7()+".png")
	defer func() { _ = os.Remove(tempFile) }()

	winPath := tempFile
	if wsl {
		result := run(ctx, "wslpath", []string{"-w", tempFile}, clipboardImageListTimeoutMS)
		if result == nil {
			return nil, false
		}
		winPath = strings.TrimSpace(string(result))
		if winPath == "" {
			return nil, false
		}
	}
	quoted := strings.ReplaceAll(winPath, "'", "''")
	script := strings.Join([]string{
		"Add-Type -AssemblyName System.Windows.Forms",
		"Add-Type -AssemblyName System.Drawing",
		"$path = '" + quoted + "'",
		"$img = [System.Windows.Forms.Clipboard]::GetImage()",
		"if ($img) { $img.Save($path, [System.Drawing.Imaging.ImageFormat]::Png); Write-Output 'ok' } else { Write-Output 'empty' }",
	}, "; ")
	result := run(ctx, "powershell.exe", []string{"-NoProfile", "-Command", script}, clipboardImagePowerShellTimeoutMS)
	if result == nil || strings.TrimSpace(string(result)) != "ok" {
		return nil, false
	}
	bytes, err := os.ReadFile(tempFile)
	if err != nil || len(bytes) == 0 {
		return nil, false
	}
	return &ClipboardImage{Bytes: bytes, MimeType: "image/png"}, false
}

// readClipboardImageViaPngpaste is the macOS substitute for the native
// clipboard: pngpaste writes the clipboard image to stdout.
func readClipboardImageViaPngpaste(ctx context.Context, run clipboardImageRunner) (*ClipboardImage, bool) {
	data := run(ctx, "pngpaste", []string{"-"}, 0)
	if len(data) == 0 {
		return nil, false
	}
	return &ClipboardImage{Bytes: data, MimeType: "image/png"}, false
}

func encodeBytesAsPNG(data []byte, mimeType string) ([]byte, bool) {
	encoded, ok := EncodeImageAsPNG(base64.StdEncoding.EncodeToString(data), mimeType)
	if !ok {
		return nil, false
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, false
	}
	return decoded, true
}

// WriteClipboardImageTempFile writes clipboard image bytes to a temp file whose
// extension matches the MIME type, returning the path. Callers insert the path
// so the existing @file image attachment path handles it.
func WriteClipboardImageTempFile(image *ClipboardImage) (string, error) {
	extension := ExtensionForImageMimeType(image.MimeType)
	if extension == "" {
		extension = "png"
	}
	path := filepath.Join(os.TempDir(), AppName+"-clipboard-"+UUIDv7()+"."+extension)
	if err := os.WriteFile(path, image.Bytes, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// ReadClipboardFilePathsContext reads file paths copied to the clipboard, or
// nil when there are none. Upstream readClipboardFilePaths is native-only; the
// port reads the platform URI-list targets instead (text/uri-list under
// Wayland/X11, the Windows FileDropList through PowerShell). macOS has no
// command substitute and reports none.
func ReadClipboardFilePathsContext(ctx context.Context) ([]string, error) {
	runner := func(ctx context.Context, command string, args []string, timeoutMS int) []byte {
		return runClipboardCommand(ctx, command, args, clipboardCommandOptions{timeoutMS: timeoutMS})
	}
	return readClipboardFilePathsWith(ctx, runtime.GOOS, os.Getenv, runner)
}

func readClipboardFilePathsWith(ctx context.Context, goos string, getenv func(string) string, run clipboardImageRunner) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch goos {
	case "linux":
		if IsWaylandSession(getenv) {
			types := run(ctx, "wl-paste", []string{"--list-types"}, clipboardImageListTimeoutMS)
			if containsURIList(types) {
				data := run(ctx, "wl-paste", []string{"--type", "text/uri-list", "--no-newline"}, 0)
				if paths := parseURIList(data); len(paths) > 0 {
					return paths, nil
				}
			}
		}
		if getenv("DISPLAY") != "" {
			targets := run(ctx, "xclip", []string{"-selection", "clipboard", "-t", "TARGETS", "-o"}, clipboardImageListTimeoutMS)
			if containsURIList(targets) {
				data := run(ctx, "xclip", []string{"-selection", "clipboard", "-t", "text/uri-list", "-o"}, 0)
				if paths := parseURIList(data); len(paths) > 0 {
					return paths, nil
				}
			}
		}
	case "windows":
		output := run(ctx, "powershell.exe", []string{
			"-NoProfile", "-Command",
			"Get-Clipboard -Format FileDropList | ForEach-Object { $_.FullName }",
		}, clipboardImagePowerShellTimeoutMS)
		if paths := parsePathLines(output); len(paths) > 0 {
			return paths, nil
		}
	}
	return nil, nil
}

func containsURIList(data []byte) bool {
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "text/uri-list" {
			return true
		}
	}
	return false
}

func parsePathLines(data []byte) []string {
	var paths []string
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			paths = append(paths, trimmed)
		}
	}
	return paths
}

// parseURIList reads an RFC 2483 text/uri-list payload: comment lines start
// with '#', and file URLs become local paths. Plain lines pass through.
func parseURIList(data []byte) []string {
	var paths []string
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !strings.HasPrefix(trimmed, "file://") {
			paths = append(paths, trimmed)
			continue
		}
		parsed, err := url.Parse(trimmed)
		if err != nil || parsed.Path == "" {
			continue
		}
		paths = append(paths, parsed.Path)
	}
	return paths
}
