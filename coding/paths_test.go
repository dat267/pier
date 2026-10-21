package coding

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// utils/paths.ts isLocalPath excludes builtin: alongside package and URL prefixes.
func TestIsLocalPath(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"./skills", true},
		{"/abs/skills", true},
		{"~/skills", true},
		{"skills", true},
		// file: URLs are local, as upstream resolves them.
		{"file:///tmp/skills", true},
		{"npm:some-package", false},
		{"git:github.com/x/y", false},
		{"github:x/y", false},
		{"https://example.com/skills.tgz", false},
		{"http://example.com/skills.tgz", false},
		{"ssh:host/path", false},
		{"builtin:core", false},
		{"\uFEFFbuiltin:core\uFEFF", false},
		{"  npm:padded  ", false},
	}
	for _, tc := range cases {
		if got := IsLocalPath(tc.in); got != tc.want {
			t.Errorf("IsLocalPath(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// Reference: pi v1.3.0 utils/paths.ts resolvePath calls node:path.resolve,
// which makes even an empty or relative base absolute against process.cwd().
func TestResolvePathWithRelativeBaseIsAbsolute(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		input, base, suffix string
	}{
		{"", "", ""},
		{"file.txt", "", "file.txt"},
		{"file.txt", "subdir", filepath.Join("subdir", "file.txt")},
		{"../file.txt", "subdir", "file.txt"},
	} {
		if got, want := ResolvePath(tc.input, tc.base, PathInputOptions{}), filepath.Join(cwd, tc.suffix); got != want {
			t.Errorf("ResolvePath(%q, %q) = %q; want %q", tc.input, tc.base, got, want)
		}
	}
}

func TestResolveCLIPaths(t *testing.T) {
	cwd := t.TempDir()

	if got := ResolveCLIPaths(cwd, nil); got != nil {
		t.Errorf("ResolveCLIPaths(nil) = %#v, want nil", got)
	}

	// Use a path that is absolute on this platform. "/abs/skills" is neither
	// absolute nor rooted-with-a-drive on Windows, and Node's path.isAbsolute
	// agrees, so the port resolves it against cwd exactly as upstream would.
	// ("\abs\skills" would not do either: Windows calls that rooted, not
	// absolute.)
	absolute := "/abs/skills"
	if runtime.GOOS == "windows" {
		absolute = filepath.VolumeName(cwd) + `\abs\skills`
	}

	got := ResolveCLIPaths(cwd, []string{"./skills", "npm:pkg", absolute})
	want := []string{filepath.Join(cwd, "skills"), "npm:pkg", absolute}
	if len(got) != len(want) {
		t.Fatalf("ResolveCLIPaths = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ResolveCLIPaths[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// Port of utils/paths.ts normalizeWindowsShellPath: MSYS/Git-Bash/WSL mount
// paths become native drive paths.
func TestNormalizeWindowsShellPath(t *testing.T) {
	for input, want := range map[string]string{
		"/mnt/c/Users/me/file.txt": "C:\\Users\\me\\file.txt",
		"/cygdrive/d/tmp":          "D:\\tmp",
		"/c/Users/Me":              "C:\\Users\\Me",
		"/C/Users":                 "C:\\Users",
		"/c":                       "C:\\",
		"/c/":                      "C:\\",
		"//server/share":           "//server/share",
		"/c/Users\\me":             "/c/Users\\me",
		`C:\Users`:                 `C:\Users`,
		"relative/path":            "relative/path",
		"/notadrive/x":             "/notadrive/x",
		"/mnt/cc/x":                "/mnt/cc/x",
	} {
		if got := NormalizeWindowsShellPath(input); got != want {
			t.Errorf("%q: got %q want %q", input, got, want)
		}
	}
}

// utils/paths.ts normalizes trimmed paths with JavaScript String.trim.
func TestNormalizePathTrimsJavaScriptWhitespace(t *testing.T) {
	home := t.TempDir()
	got := NormalizePath("\uFEFF~/project\uFEFF", PathInputOptions{Trim: true, HomeDir: home})
	want := filepath.Join(home, "project")
	if got != want {
		t.Fatalf("NormalizePath() = %q; want %q", got, want)
	}
}

// Reference: pi v1.3.0 utils/paths.ts uses node:url.fileURLToPath.
// URL search/hash are metadata, while escaped ?/# remain literal filename bytes.
func TestNormalizePathFileURLIgnoresQueryAndFragment(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"file:///tmp/skills?download=1", "/tmp/skills"},
		{"file:///tmp/skills#section", "/tmp/skills"},
		{"file://LOCALHOST/tmp/skills?download=1#section", "/tmp/skills"},
		{"file:///tmp/skills%3Fv%23a?download=1#section", "/tmp/skills?v#a"},
	} {
		if got := NormalizePath(tc.input, PathInputOptions{}); got != tc.want {
			t.Errorf("NormalizePath(%q) = %q; want %q", tc.input, got, tc.want)
		}
	}
}

// normalizePath resolves file:// URLs through Node's fileURLToPath, whose URL
// parser lowercases the hostname, so `file://LOCALHOST/path` is a local path.
// The port compared the raw host against "localhost".
func TestNormalizePathAcceptsUppercaseLocalhostFileURL(t *testing.T) {
	if got := NormalizePath("file://LOCALHOST/tmp/skills", PathInputOptions{}); got != "/tmp/skills" {
		t.Fatalf("NormalizePath(file://LOCALHOST) = %q; want %q", got, "/tmp/skills")
	}
	if got := NormalizePath("file://localhost/tmp/skills", PathInputOptions{}); got != "/tmp/skills" {
		t.Fatalf("NormalizePath(file://localhost) = %q; want %q", got, "/tmp/skills")
	}
}

// Reference: pi v1.3.0 utils/paths.ts calls node:url.fileURLToPath, so the URL
// parser normalizes the pathname before the path is decoded: dot segments
// (literal or percent-encoded) resolve, backslashes are separators for the
// special file scheme, and an empty path is the root. Each expectation was read
// off the reference.
func TestNormalizePathFileURLPathname(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"file://", "/"},
		{"file://localhost", "/"},
		{"file:///?q", "/"},
		{"file:///tmp/a/../b", "/tmp/b"},
		{"file:///tmp/a/..", "/tmp/"},
		{"file:///tmp/a/../", "/tmp/"},
		{"file:///..", "/"},
		{"file:///tmp/a/../..", "/"},
		{"file:///tmp/a/../../..", "/"},
		{"file:///tmp//a", "/tmp//a"},
		{"file:///tmp/a/./", "/tmp/a/"},
		{"file:///tmp/a/.../b", "/tmp/a/.../b"},
		{"file:///tmp/a/%2e%2e/b", "/tmp/b"},
		{"file:///tmp/a/.%2e/b", "/tmp/b"},
		{"file:///tmp/a/%2e./b", "/tmp/b"},
		{"file:///tmp/a/%2e%2E/b", "/tmp/b"},
		{"file:///tmp/a/...%2e/b", "/tmp/a/..../b"},
		{"file:///tmp/a/.%2E%2E/b", "/tmp/a/.../b"},
		{"file:///tmp/a/%2E/b", "/tmp/a/b"},
		{"file:///tmp\\a\\..\\b", "/tmp/b"},
	} {
		if got := NormalizePath(tc.input, PathInputOptions{}); got != tc.want {
			t.Errorf("NormalizePath(%q) = %q; want %q", tc.input, got, tc.want)
		}
	}
}

// Reference: pi v1.3.0 utils/paths.ts calls node:url.fileURLToPath, which
// validates encoded separators in the normalized URL pathname, not in path
// segments already removed by "..".
func TestNormalizePathFileURLRemovesEncodedSlashBeforeValidation(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"file:///tmp/%2f/../safe", "/tmp/safe"},
		{"file:///tmp/%2F/..", "/tmp/"},
		{"file:///tmp/%2f/safe", "file:///tmp/%2f/safe"},
	} {
		if got := NormalizePath(tc.input, PathInputOptions{}); got != tc.want {
			t.Errorf("NormalizePath(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// Reference: pi v1.3.0 utils/paths.ts -> node:url.fileURLToPath rejects invalid
// percent-encoded UTF-8. D261 keeps a rejected URL unchanged instead of throwing.
func TestNormalizePathFileURLRejectsInvalidUTF8(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"file:///tmp/%FF", "file:///tmp/%FF"},
		{"file:///tmp/%C0%AF", "file:///tmp/%C0%AF"},
		{"file:///tmp/%ED%A0%80", "file:///tmp/%ED%A0%80"},
		{"file:///tmp/%E2%82", "file:///tmp/%E2%82"},
		{"file:///tmp/%FF/../safe", "/tmp/safe"},
		{"file:///tmp/%E2%82%AC", "/tmp/€"},
	} {
		if got := NormalizePath(tc.input, PathInputOptions{}); got != tc.want {
			t.Errorf("NormalizePath(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// Reference: pi v1.3.0 utils/paths.ts -> node:url.fileURLToPath uses the WHATWG
// parser: raw tabs/newlines disappear and trailing C0 controls/spaces are
// stripped, while percent-encoded whitespace remains part of the filename.
func TestNormalizePathFileURLIgnoresParserWhitespace(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"file:///tmp/a\tb\nc\rd", "/tmp/abcd"},
		{"file://local\thost/tmp/a", "/tmp/a"},
		{"file:///tmp/a \r\n", "/tmp/a"},
		{"file:///tmp/a\x00\x1f ", "/tmp/a"},
		{"file:///tmp/a%20%09%0a", "/tmp/a \t\n"},
	} {
		if got := NormalizePath(tc.input, PathInputOptions{}); got != tc.want {
			t.Errorf("NormalizePath(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// Reference: pi v1.3.0 utils/paths.ts -> node:url.fileURLToPath normalizes the
// URL host, including percent escapes, before recognizing localhost.
func TestNormalizePathFileURLAcceptsEscapedLocalhost(t *testing.T) {
	for _, input := range []string{
		"file://%6cocalhost/tmp/a",
		"file://local%68ost/tmp/a",
		"file://%4cOCALHOST/tmp/a",
	} {
		if got := NormalizePath(input, PathInputOptions{}); got != "/tmp/a" {
			t.Errorf("NormalizePath(%q) = %q, want /tmp/a", input, got)
		}
	}
}

// Reference: pi v1.3.0 utils/paths.ts -> node:url.fileURLToPath on POSIX.
// The WHATWG file parser recognizes drive letters even on POSIX, normalizes
// the legacy C| spelling, and does not let ".." remove the drive root.
func TestNormalizePathFileURLRecognizesDriveRoots(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"file://C:/tmp/a", "/C:/tmp/a"},
		{"file://C:", "/C:"},
		{"file://C|/tmp/a", "/C:/tmp/a"},
		{"file:///C|/tmp/a", "/C:/tmp/a"},
		{"file:///C:/..", "/C:/"},
		{"file:///C:/tmp/../..", "/C:/"},
		{"file:///C:/../../a", "/C:/a"},
		{"file:///tmp/C|/a", "/tmp/C|/a"},
	} {
		if got := NormalizePath(tc.input, PathInputOptions{}); got != tc.want {
			t.Errorf("NormalizePath(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// TestNormalizePathKeepsAnInvalidFileURL pins D261: fileURLToPath throws for a
// non-local host, an encoded slash or malformed percent encoding, and
// normalizePath propagates the exception. NormalizePath has no error result, so
// it returns the input unchanged and the caller fails on the path instead.
func TestNormalizePathKeepsAnInvalidFileURL(t *testing.T) {
	for _, input := range []string{
		"file://host/tmp/x",
		"file:///tmp/a%2Fb",
		"file:///tmp/%zz",
		"file://..",
	} {
		if got := NormalizePath(input, PathInputOptions{}); got != input {
			t.Errorf("NormalizePath(%q) = %q; want the input unchanged", input, got)
		}
	}
}

// Reference: pi v1.3.0 utils/paths.ts expands `~` with node:path.join, which
// keeps a trailing separator. Go's filepath.Join cleans it away, so "~///"
// lost the slash the reference keeps. Expectations read off the reference.
func TestNormalizePathTildeKeepsTrailingSeparator(t *testing.T) {
	for _, tc := range []struct{ input, want string }{
		{"~/", "/home/u"},
		{"~///", "/home/u/"},
		{"~/file://", "/home/u/file:/"},
		{"~/a/./", "/home/u/a/"},
		{"~/a/..", "/home/u"},
		{"~/a/../", "/home/u/"},
		{"~//b", "/home/u/b"},
		{"~/a//", "/home/u/a/"},
		{"~/.", "/home/u"},
		{"~/..", "/home"},
	} {
		got := NormalizePath(tc.input, PathInputOptions{HomeDir: "/home/u"})
		if got != tc.want {
			t.Errorf("NormalizePath(%q) = %q; want %q", tc.input, got, tc.want)
		}
	}
}
