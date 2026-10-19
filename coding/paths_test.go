package coding

import (
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
