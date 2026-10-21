package coding

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/dat267/pier/ai"
	"golang.org/x/text/unicode/norm"
)

// Port of utils/paths.ts and core/tools/path-utils.ts.

var unicodeSpaces = regexp.MustCompile(`[\x{00A0}\x{2000}-\x{200A}\x{202F}\x{205F}\x{3000}]`)

// PathInputOptions tune path normalization.
type PathInputOptions struct {
	// Trim strips leading/trailing whitespace first.
	Trim bool
	// ExpandTilde expands a leading ~. Nil means upstream's default (true).
	ExpandTilde *bool
	// HomeDir overrides the home directory for ~ expansion.
	HomeDir string
	// StripAtPrefix strips a leading @ (CLI @file paths).
	StripAtPrefix bool
	// NormalizeUnicodeSpaces rewrites unicode space variants to spaces.
	NormalizeUnicodeSpaces bool
}

// CanonicalizePath resolves to the canonical (real) form following symlinks,
// falling back to the raw path when resolution fails (port of
// canonicalizePath).
func CanonicalizePath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

// GetFileRevision returns an opaque file revision string used to detect
// changes without reading the file (port of getFileRevision). It returns
// ("", false) when the file cannot be stat'ed.
//
// dev/ino come from the platform stat structure; on platforms that do not
// expose them (Windows) they are zero, and the size/timestamps still make the
// revision change-detecting.
func GetFileRevision(path string) (string, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return "", false
	}
	dev, ino := statDeviceInode(info)
	return fmt.Sprintf("%d:%d:%d:%d:%d", dev, ino, info.Size(),
		info.ModTime().UnixNano(), statChangeTimeNano(info)), true
}

// PathExists reports whether the path is accessible.
func PathExists(filePath string) bool {
	_, err := os.Stat(filePath)
	return err == nil
}

func homeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
}

// NormalizePath normalizes path input: trim, unicode-space folding, @-prefix
// stripping, the Windows shell-path rewrite, ~ expansion, and file:// URLs
// (port of normalizePath).
func NormalizePath(input string, options PathInputOptions) string {
	normalized := input
	if options.Trim {
		normalized = ai.JSTrim(normalized)
	}
	if options.NormalizeUnicodeSpaces {
		normalized = unicodeSpaces.ReplaceAllString(normalized, " ")
	}
	if options.StripAtPrefix && strings.HasPrefix(normalized, "@") {
		normalized = normalized[1:]
	}
	if runtime.GOOS == "windows" {
		normalized = NormalizeWindowsShellPath(normalized)
	}
	expandTilde := true
	if options.ExpandTilde != nil {
		expandTilde = *options.ExpandTilde
	}
	if expandTilde {
		home := options.HomeDir
		if home == "" {
			home = homeDir()
		}
		if normalized == "~" {
			return home
		}
		if strings.HasPrefix(normalized, "~/") || (runtime.GOOS == "windows" && strings.HasPrefix(normalized, "~\\")) {
			return joinExpandedHome(home, normalized[2:])
		}
	}
	if strings.HasPrefix(normalized, "file://") {
		if path, err := filePathFromURL(normalized); err == nil {
			return path
		}
	}
	return normalized
}

// joinExpandedHome joins the home directory with the rest of a `~/` path the way
// Node's path.join does: dot segments resolve, but a trailing separator survives
// unless the last segment is "." or "..". filepath.Join always cleans it away.
func joinExpandedHome(home, rest string) string {
	joined := filepath.Join(home, rest)
	trailing := "/"
	if runtime.GOOS == "windows" {
		trailing = "\\"
	}
	separatorSuffix := strings.HasSuffix(rest, "/")
	if runtime.GOOS == "windows" && strings.HasSuffix(rest, "\\") {
		separatorSuffix = true
	}
	if !separatorSuffix {
		return joined
	}
	if strings.HasSuffix(joined, trailing) {
		return joined
	}
	return joined + trailing
}

var windowsShellPathPattern = regexp.MustCompile(`(?i)^/(?:mnt/|cygdrive/)?([a-z])(?:/(.*))?$`)

// NormalizeWindowsShellPath rewrites an MSYS/Git-Bash/WSL mount path such as
// /c/Users/me, /mnt/c/Users/me or /cygdrive/c/Users/me to C:\Users\me
// (upstream normalizeWindowsShellPath). A UNC path, a path with a backslash,
// or anything else is returned unchanged.
func NormalizeWindowsShellPath(filePath string) string {
	if !strings.HasPrefix(filePath, "/") || strings.HasPrefix(filePath, "//") || strings.Contains(filePath, "\\") {
		return filePath
	}
	match := windowsShellPathPattern.FindStringSubmatch(filePath)
	if match == nil {
		return filePath
	}
	suffix := ""
	if match[2] != "" {
		suffix = strings.ReplaceAll(match[2], "/", "\\")
	}
	return strings.ToUpper(match[1]) + ":\\" + suffix
}

// pathToFileURL converts an absolute path to a file:// URL the way Node's
// pathToFileURL does (runtime.ts advertises pathToFileURL(cwd).href): spaces,
// `#`, `?` and `%` are percent-encoded, and on Windows a drive path becomes
// file:///C:/... while a UNC path keeps its host.
func pathToFileURL(path string) string {
	slashed := filepath.ToSlash(path)
	if runtime.GOOS == "windows" {
		if strings.HasPrefix(slashed, "//") {
			// A UNC path already names the authority: //host/share/... .
			return "file:" + (&url.URL{Path: slashed}).EscapedPath()
		}
		if len(slashed) >= 2 && slashed[1] == ':' {
			slashed = "/" + slashed
		}
	} else if !strings.HasPrefix(slashed, "/") {
		slashed = "/" + slashed
	}
	return "file://" + (&url.URL{Path: slashed}).EscapedPath()
}

// filePathFromURL converts a file:// URL to a path (fileURLToPath).
func filePathFromURL(url string) (string, error) {
	const scheme = "file://"
	if !strings.HasPrefix(url, scheme) {
		return "", fmtPathError("not a file URL")
	}
	// WHATWG strips trailing C0 controls/spaces and raw tabs/newlines. A
	// backslash is a path separator for the special file scheme.
	url = strings.TrimRightFunc(url, func(r rune) bool { return r <= 0x20 })
	path := strings.Map(func(r rune) rune {
		switch r {
		case '\t', '\r', '\n':
			return -1
		case '\\':
			return '/'
		default:
			return r
		}
	}, url[len(scheme):])
	// Strip URL metadata before percent-decoding: encoded ?/# are filename
	// characters, not search/hash delimiters (Node fileURLToPath).
	if index := strings.IndexAny(path, "?#"); index >= 0 {
		path = path[:index]
	}
	// file:///abs/path → path starts with '/' (empty host); file://host/path
	// carries a host component. An empty path is the root.
	if path == "" {
		return "/", nil
	}
	if !strings.HasPrefix(path, "/") {
		idx := strings.Index(path, "/")
		if idx < 0 {
			idx = len(path)
		}
		host := path[:idx]
		if isURLWindowsDriveLetter(host) {
			path = "/" + path
		} else {
			path = path[idx:]
			if path == "" {
				path = "/"
			}
			// WHATWG decodes and lowercases the hostname before checking localhost.
			decodedHost, err := percentDecode(host)
			if err != nil {
				return "", err
			}
			if !strings.EqualFold(decodedHost, "localhost") {
				return "", fmtPathError("file URL with non-localhost host")
			}
		}
	}
	path = normalizeURLPathname(path)
	// Percent-encoded slashes are rejected after URL normalization and before decoding (Node fileURLToPath:
	// "File URL path must not include encoded / characters").
	if hasEncodedSlash(path) {
		return "", fmtPathError("file URL path with an encoded slash")
	}
	decoded, err := percentDecode(path)
	if err != nil {
		return "", err
	}
	if !utf8.ValidString(decoded) {
		return "", fmtPathError("file URL path with invalid UTF-8")
	}
	return decoded, nil
}

// hasEncodedSlash reports whether path contains a percent-encoded "/" (case
// insensitive), which fileURLToPath rejects.
func hasEncodedSlash(path string) bool {
	for index := 0; index+2 < len(path); index++ {
		if path[index] != '%' {
			continue
		}
		if path[index+1] == '2' && (path[index+2] == 'f' || path[index+2] == 'F') {
			return true
		}
	}
	return false
}

// normalizeURLPathname resolves the "." and ".." segments of a URL pathname the
// way the WHATWG URL parser does: an encoded dot counts, segments above the root
// are dropped, a trailing dot segment keeps a trailing slash, and empty segments
// are preserved. The path must be absolute.
func normalizeURLPathname(path string) string {
	segments := strings.Split(path, "/")
	out := make([]string, 0, len(segments))
	for index, segment := range segments {
		last := index == len(segments)-1
		switch {
		case isURLSingleDotSegment(segment):
			if last {
				out = append(out, "")
			}
		case isURLDoubleDotSegment(segment):
			// The first segment is the empty prefix before the root slash.
			driveRoot := len(out) == 2 && isURLWindowsDriveLetter(out[1])
			if len(out) > 1 && !driveRoot {
				out = out[:len(out)-1]
			}
			if last {
				out = append(out, "")
			}
		default:
			if len(out) == 1 && out[0] == "" && isURLWindowsDriveLetter(segment) {
				segment = segment[:1] + ":"
			}
			out = append(out, segment)
		}
	}
	return strings.Join(out, "/")
}

func isURLWindowsDriveLetter(segment string) bool {
	return len(segment) == 2 && ((segment[0] >= 'a' && segment[0] <= 'z') ||
		(segment[0] >= 'A' && segment[0] <= 'Z')) && (segment[1] == ':' || segment[1] == '|')
}

// isURLSingleDotSegment reports "." spelled with a literal or encoded dot
// (WHATWG single-dot path segment).
func isURLSingleDotSegment(segment string) bool {
	return segment == "." || strings.EqualFold(segment, "%2e")
}

// isURLDoubleDotSegment reports ".." where either dot may be encoded.
func isURLDoubleDotSegment(segment string) bool {
	dots := 0
	for index := 0; index < len(segment); {
		switch {
		case index+3 <= len(segment) && segment[index] == '%' && segment[index+1] == '2' &&
			(segment[index+2] == 'e' || segment[index+2] == 'E'):
			index += 3
		case segment[index] == '.':
			index++
		default:
			return false
		}
		dots++
	}
	return dots == 2
}

func percentDecode(s string) (string, error) {
	var out []byte
	for i := 0; i < len(s); i++ {
		if s[i] == '%' {
			if i+2 >= len(s) {
				return "", fmtPathError("invalid percent encoding")
			}
			hi := hexValue(s[i+1])
			lo := hexValue(s[i+2])
			if hi < 0 || lo < 0 {
				return "", fmtPathError("invalid percent encoding")
			}
			out = append(out, byte(hi<<4|lo))
			i += 2
			continue
		}
		out = append(out, s[i])
	}
	return string(out), nil
}

func hexValue(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	default:
		return -1
	}
}

type pathError string

func (e pathError) Error() string { return string(e) }

func fmtPathError(msg string) error { return pathError(msg) }

// ResolvePath resolves a path relative to baseDir (port of resolvePath):
// absolute paths are cleaned; relative paths resolve against the base.
func ResolvePath(input string, baseDir string, options PathInputOptions) string {
	normalized := NormalizePath(input, options)
	normalizedBase := NormalizePath(baseDir, PathInputOptions{})
	if filepath.IsAbs(normalized) {
		return filepath.Clean(normalized)
	}
	resolved := filepath.Join(normalizedBase, normalized)
	if absolute, err := filepath.Abs(resolved); err == nil {
		return absolute
	}
	return filepath.Clean(resolved)
}

// ExpandPath normalizes with unicode-space folding and @-stripping
// (port of expandPath).
func ExpandPath(filePath string) string {
	return NormalizePath(filePath, PathInputOptions{
		NormalizeUnicodeSpaces: true,
		StripAtPrefix:          true,
	})
}

// ResolveToCwd resolves a path relative to cwd (port of resolveToCwd).
func ResolveToCwd(filePath string, cwd string) string {
	return ResolvePath(filePath, cwd, PathInputOptions{
		NormalizeUnicodeSpaces: true,
		StripAtPrefix:          true,
	})
}

// macOS filename fallbacks (port of path-utils.ts).

const narrowNoBreakSpace = "\u202F"

// macOSScreenshotTimeRegex matches the space before an AM/PM marker, which
// macOS screenshot file names join with a narrow no-break space.
var macOSScreenshotTimeRegex = regexp.MustCompile(` (?i)(AM|PM)\.`)

func tryMacOSScreenshotPath(filePath string) string {
	return macOSScreenshotTimeRegex.ReplaceAllString(filePath, narrowNoBreakSpace+"$1.")
}

func tryNFDVariant(filePath string) string {
	return norm.NFD.String(filePath)
}

func tryCurlyQuoteVariant(filePath string) string {
	return strings.ReplaceAll(filePath, "'", "\u2019")
}

// ResolveReadPath resolves a read path, trying macOS filename variants when
// the resolved path does not exist (port of resolveReadPath).
func ResolveReadPath(filePath string, cwd string) string {
	resolved := ResolveToCwd(filePath, cwd)

	if fileExists(resolved) {
		return resolved
	}

	amPmVariant := tryMacOSScreenshotPath(resolved)
	if amPmVariant != resolved && fileExists(amPmVariant) {
		return amPmVariant
	}

	nfdVariant := tryNFDVariant(resolved)
	if nfdVariant != resolved && fileExists(nfdVariant) {
		return nfdVariant
	}

	curlyVariant := tryCurlyQuoteVariant(resolved)
	if curlyVariant != resolved && fileExists(curlyVariant) {
		return curlyVariant
	}

	nfdCurlyVariant := tryCurlyQuoteVariant(nfdVariant)
	if nfdCurlyVariant != resolved && fileExists(nfdCurlyVariant) {
		return nfdCurlyVariant
	}

	return resolved
}

func fileExists(filePath string) bool {
	_, err := os.Stat(filePath)
	return err == nil
}

// GetCwdRelativePath returns the path relative to cwd when inside it
// (port of getCwdRelativePath); "" otherwise.
func GetCwdRelativePath(filePath string, cwd string) (string, bool) {
	resolvedCwd := ResolvePath(cwd, "", PathInputOptions{})
	resolvedPath := ResolvePath(filePath, resolvedCwd, PathInputOptions{})
	rel, err := filepath.Rel(resolvedCwd, resolvedPath)
	if err != nil {
		return "", false
	}
	isInsideCwd := rel == "" || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel))
	if !isInsideCwd {
		return "", false
	}
	if rel == "" {
		return ".", true
	}
	return rel, true
}

// IsLocalPath reports whether a resource argument names a filesystem path
// rather than a package source (port of isLocalPath). A file: URL counts as
// local, as upstream resolves it.
func IsLocalPath(value string) bool {
	trimmed := ai.JSTrim(value)
	for _, prefix := range []string{"npm:", "git:", "github:", "http:", "https:", "ssh:", "builtin:"} {
		if strings.HasPrefix(trimmed, prefix) {
			return false
		}
	}
	return true
}

// ResolveCLIPaths resolves the local entries of a CLI resource-path list
// against cwd, leaving package sources untouched so a loader can reject them
// with its own message (port of resolveCliPaths). Nil stays nil.
func ResolveCLIPaths(cwd string, paths []string) []string {
	if paths == nil {
		return nil
	}
	out := make([]string, len(paths))
	for i, path := range paths {
		if IsLocalPath(path) {
			out[i] = ResolvePath(path, cwd, PathInputOptions{})
			continue
		}
		out[i] = path
	}
	return out
}
