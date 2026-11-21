package coding

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestSelectPreferredImageMimeType(t *testing.T) {
	if got := selectPreferredImageMimeType([]string{"text/plain", "image/jpeg", "image/png"}); got != "image/png" {
		t.Fatalf("preference = %q", got)
	}
	if got := selectPreferredImageMimeType([]string{"image/bmp"}); got != "image/bmp" {
		t.Fatalf("fallback image type = %q", got)
	}
	if got := selectPreferredImageMimeType([]string{"text/plain", ""}); got != "" {
		t.Fatalf("non-image = %q", got)
	}
}

func TestExtensionForImageMimeType(t *testing.T) {
	for mimeType, want := range map[string]string{
		"image/png": "png", "image/jpeg": "jpg", "image/webp": "webp", "image/gif": "gif",
		"image/png; charset=binary": "png", "image/bmp": "", "text/plain": "",
	} {
		if got := ExtensionForImageMimeType(mimeType); got != want {
			t.Fatalf("%s: extension = %q want %q", mimeType, got, want)
		}
	}
}

func TestIsWaylandSessionAndWSL(t *testing.T) {
	wayland := func(key string) string {
		switch key {
		case "XDG_SESSION_TYPE":
			return "wayland"
		}
		return ""
	}
	if !IsWaylandSession(wayland) {
		t.Fatal("wayland not detected")
	}
	if IsWaylandSession(func(string) string { return "" }) {
		t.Fatal("false wayland")
	}
	if !IsWSL(func(key string) string {
		if key == "WSL_DISTRO_NAME" {
			return "Ubuntu"
		}
		return ""
	}, nil) {
		t.Fatal("env WSL not detected")
	}
	if !IsWSL(func(string) string { return "" }, func(string) ([]byte, error) {
		return []byte("Linux version 5.15.0-microsoft-standard"), nil
	}) {
		t.Fatal("proc WSL not detected")
	}
	if IsWSL(func(string) string { return "" }, func(string) ([]byte, error) {
		return nil, errors.New("missing")
	}) {
		t.Fatal("false WSL")
	}
}

// clipboardImageRunnerStub records the commands and returns scripted output.
type clipboardImageRunnerStub struct {
	calls  []string
	output map[string][]byte
}

func (stub *clipboardImageRunnerStub) run(_ context.Context, command string, args []string, _ int) []byte {
	stub.calls = append(stub.calls, command+" "+strings.Join(args, " "))
	return stub.output[command+" "+strings.Join(args, " ")]
}

func noWSL(string) ([]byte, error) { return []byte("Linux version"), nil }

func TestReadClipboardImageWayland(t *testing.T) {
	stub := &clipboardImageRunnerStub{output: map[string][]byte{
		"wl-paste --list-types":                  []byte("text/plain\nimage/png\n"),
		"wl-paste --type image/png --no-newline": []byte("PNGBYTES"),
	}}
	env := func(key string) string {
		if key == "WAYLAND_DISPLAY" {
			return "wayland-0"
		}
		return ""
	}
	image, err := readClipboardImageWith(context.Background(), "linux", env, stub.run, noWSL)
	if err != nil || image == nil || string(image.Bytes) != "PNGBYTES" || image.MimeType != "image/png" {
		t.Fatalf("image = %+v err = %v", image, err)
	}
	if len(stub.calls) != 2 {
		t.Fatalf("calls = %v", stub.calls)
	}
}

// An empty Wayland clipboard must not fall through to stale X11 contents.
func TestReadClipboardImageWaylandEmptyDoesNotUseXclip(t *testing.T) {
	stub := &clipboardImageRunnerStub{output: map[string][]byte{
		"wl-paste --list-types": []byte("text/plain\n"),
	}}
	env := func(key string) string {
		if key == "WAYLAND_DISPLAY" {
			return "wayland-0"
		}
		return ""
	}
	image, err := readClipboardImageWith(context.Background(), "linux", env, stub.run, noWSL)
	if err != nil || image != nil {
		t.Fatalf("image = %+v err = %v", image, err)
	}
	for _, call := range stub.calls {
		if strings.HasPrefix(call, "xclip") {
			t.Fatalf("xclip was consulted: %v", stub.calls)
		}
	}
}

// A missing wl-paste falls through to xclip.
func TestReadClipboardImageFallsBackToXclip(t *testing.T) {
	stub := &clipboardImageRunnerStub{output: map[string][]byte{
		"xclip -selection clipboard -t TARGETS -o":   []byte("image/png\n"),
		"xclip -selection clipboard -t image/png -o": []byte("XCLIPBYTES"),
	}}
	env := func(key string) string {
		if key == "WAYLAND_DISPLAY" {
			return "wayland-0"
		}
		return ""
	}
	image, err := readClipboardImageWith(context.Background(), "linux", env, stub.run, noWSL)
	if err != nil || image == nil || string(image.Bytes) != "XCLIPBYTES" {
		t.Fatalf("image = %+v err = %v", image, err)
	}
	if !reflect.DeepEqual(stub.calls[:1], []string{"wl-paste --list-types"}) {
		t.Fatalf("calls = %v", stub.calls)
	}
}

func TestReadClipboardImageTermuxIsSkipped(t *testing.T) {
	stub := &clipboardImageRunnerStub{}
	image, err := readClipboardImageWith(context.Background(), "android", func(key string) string {
		if key == "TERMUX_VERSION" {
			return "0.118"
		}
		return ""
	}, stub.run, noWSL)
	if err != nil || image != nil || len(stub.calls) != 0 {
		t.Fatalf("image = %+v calls = %v", image, stub.calls)
	}
}

func TestWriteClipboardImageTempFile(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	path, err := WriteClipboardImageTempFile(&ClipboardImage{Bytes: []byte("PNG"), MimeType: "image/jpeg"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, ".jpg") {
		t.Fatalf("path = %q", path)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "PNG" {
		t.Fatalf("data = %q err = %v", data, err)
	}
}

func TestReadClipboardFilePathsWayland(t *testing.T) {
	stub := &clipboardImageRunnerStub{output: map[string][]byte{
		"wl-paste --list-types":                      []byte("text/plain\ntext/uri-list\n"),
		"wl-paste --type text/uri-list --no-newline": []byte("# comment\r\nfile:///home/u/a.png\r\nfile:///home/u/b%20c.txt\r\n"),
	}}
	env := func(key string) string {
		if key == "WAYLAND_DISPLAY" {
			return "wayland-0"
		}
		return ""
	}
	paths, err := readClipboardFilePathsWith(context.Background(), "linux", env, stub.run)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"/home/u/a.png", "/home/u/b c.txt"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %v want %v", paths, want)
	}
}

func TestReadClipboardFilePathsXclip(t *testing.T) {
	stub := &clipboardImageRunnerStub{output: map[string][]byte{
		"xclip -selection clipboard -t TARGETS -o":       []byte("TIMESTAMP\ntext/uri-list\n"),
		"xclip -selection clipboard -t text/uri-list -o": []byte("file:///tmp/one.txt"),
	}}
	env := func(key string) string {
		if key == "DISPLAY" {
			return ":0"
		}
		return ""
	}
	paths, err := readClipboardFilePathsWith(context.Background(), "linux", env, stub.run)
	if err != nil || !reflect.DeepEqual(paths, []string{"/tmp/one.txt"}) {
		t.Fatalf("paths = %v err = %v", paths, err)
	}
}

func TestReadClipboardFilePathsNone(t *testing.T) {
	stub := &clipboardImageRunnerStub{output: map[string][]byte{
		"wl-paste --list-types": []byte("text/plain\n"),
	}}
	env := func(string) string { return "" }
	if paths, err := readClipboardFilePathsWith(context.Background(), "linux", env, stub.run); err != nil || paths != nil {
		t.Fatalf("paths = %v err = %v", paths, err)
	}
	if paths, err := readClipboardFilePathsWith(context.Background(), "darwin", env, stub.run); err != nil || paths != nil {
		t.Fatalf("darwin paths = %v err = %v", paths, err)
	}
}

// pi v1.5.2 (b4afdddf) utils/clipboard-image.ts readClipboardImageViaPowerShell:
// the WSL path is translated with wslpath, a nil result or an empty translation
// aborts before PowerShell runs, and a translation containing a quote is
// doubled inside the single-quoted PowerShell string.
func TestReadClipboardImageViaPowerShellTranslatesThePathForWSL(t *testing.T) {
	var scripts []string
	run := func(_ context.Context, command string, args []string, _ int) []byte {
		switch command {
		case "wslpath":
			return []byte("  C:\\Temp\\it's.png \r\n")
		case "powershell.exe":
			script := args[len(args)-1]
			scripts = append(scripts, script)
			return []byte("empty")
		}
		return nil
	}
	if image, failed := readClipboardImageViaPowerShell(context.Background(), run, true); image != nil || failed {
		t.Fatalf("image = %+v failed = %v", image, failed)
	}
	if len(scripts) != 1 {
		t.Fatalf("powershell ran %d times", len(scripts))
	}
	// The translated path is trimmed and its single quote doubled.
	if !strings.Contains(scripts[0], `$path = 'C:\Temp\it''s.png'`) {
		t.Fatalf("script did not carry the translated path:\n%s", scripts[0])
	}
}

// A failed wslpath translation must abort before PowerShell runs, so Windows is
// never asked to save into a Linux path it cannot resolve.
func TestReadClipboardImageViaPowerShellStopsWhenWslpathFails(t *testing.T) {
	for _, translation := range [][]byte{nil, []byte("   \r\n")} {
		var ranPowerShell bool
		run := func(_ context.Context, command string, _ []string, _ int) []byte {
			if command == "powershell.exe" {
				ranPowerShell = true
			}
			return translation
		}
		if image, failed := readClipboardImageViaPowerShell(context.Background(), run, true); image != nil || failed {
			t.Fatalf("image = %+v failed = %v for translation %q", image, failed, translation)
		}
		if ranPowerShell {
			t.Fatalf("powershell ran after a failed wslpath (%q)", translation)
		}
	}
}

// Without wsl there is no wslpath step at all: the temp path is passed through.
func TestReadClipboardImageViaPowerShellWithoutWSLSkipsWslpath(t *testing.T) {
	var calls []string
	run := func(_ context.Context, command string, _ []string, _ int) []byte {
		calls = append(calls, command)
		return []byte("empty")
	}
	readClipboardImageViaPowerShell(context.Background(), run, false)
	if len(calls) != 1 || calls[0] != "powershell.exe" {
		t.Fatalf("calls = %v, want only powershell.exe", calls)
	}
}

// On Windows the platform read is the PowerShell one.
func TestReadClipboardImageOnWindowsUsesPowerShell(t *testing.T) {
	var ranPowerShell bool
	run := func(_ context.Context, command string, _ []string, _ int) []byte {
		if command == "powershell.exe" {
			ranPowerShell = true
		}
		return []byte("empty")
	}
	image, err := readClipboardImageWith(context.Background(), "windows", func(string) string { return "" }, run, noWSL)
	if err != nil || image != nil {
		t.Fatalf("image = %+v err = %v", image, err)
	}
	if !ranPowerShell {
		t.Fatal("the windows path did not use PowerShell")
	}
}

// On darwin the read is pngpaste, which writes the image to stdout; an empty
// read is no image.
func TestReadClipboardImageOnDarwinUsesPngpaste(t *testing.T) {
	stub := &clipboardImageRunnerStub{output: map[string][]byte{"pngpaste -": []byte("PNGDATA")}}
	image, err := readClipboardImageWith(context.Background(), "darwin", func(string) string { return "" }, stub.run, noWSL)
	if err != nil || image == nil || string(image.Bytes) != "PNGDATA" || image.MimeType != "image/png" {
		t.Fatalf("image = %+v err = %v", image, err)
	}
	empty := &clipboardImageRunnerStub{output: map[string][]byte{}}
	if image, err := readClipboardImageWith(context.Background(), "darwin", func(string) string { return "" }, empty.run, noWSL); err != nil || image != nil {
		t.Fatalf("empty read: image = %+v err = %v", image, err)
	}
}

// pi v1.5.2 (b4afdddf) utils/clipboard-image.ts readClipboardImage: on WSL an
// empty Linux clipboard is not proof Windows has no image, so the PowerShell
// read runs and its answer is kept. A non-empty Linux image skips it.
func TestReadClipboardImageOnWSLFallsBackToPowerShellOnlyWhenEmpty(t *testing.T) {
	wslEnv := func(key string) string {
		switch key {
		case "WSL_DISTRO_NAME":
			return "Ubuntu"
		case "WAYLAND_DISPLAY":
			return "wayland-0"
		}
		return ""
	}
	// Nothing on the Linux side: PowerShell answers with an empty clipboard, and
	// the fallback must have been attempted.
	var calls []string
	emptyRun := func(_ context.Context, command string, _ []string, _ int) []byte {
		calls = append(calls, command)
		if command == "wl-paste" {
			return []byte("text/plain\n") // list-types: no image type
		}
		return []byte("empty") // powershell: an empty Windows clipboard
	}
	if image, err := readClipboardImageWith(context.Background(), "linux", wslEnv, emptyRun, noWSLHint); err != nil || image != nil {
		t.Fatalf("image = %+v err = %v", image, err)
	}
	sawPowerShell := false
	for _, call := range calls {
		if call == "powershell.exe" {
			sawPowerShell = true
		}
	}
	if !sawPowerShell {
		t.Fatalf("the WSL fallback never ran PowerShell: %v", calls)
	}

	// A Linux image skips the fallback entirely.
	nonEmpty := &clipboardImageRunnerStub{output: map[string][]byte{
		"wl-paste --list-types":                  []byte("image/png\n"),
		"wl-paste --type image/png --no-newline": []byte("PNGBYTES"),
	}}
	image, err := readClipboardImageWith(context.Background(), "linux", wslEnv, nonEmpty.run, noWSLHint)
	if err != nil || image == nil || string(image.Bytes) != "PNGBYTES" {
		t.Fatalf("image = %+v err = %v", image, err)
	}
	for _, call := range nonEmpty.calls {
		if strings.HasPrefix(call, "powershell.exe") {
			t.Fatalf("the WSL fallback ran despite a Linux image: %v", nonEmpty.calls)
		}
	}
}

// noWSLHint reports a WSL environment from the kernel-release hint that
// IsWSL reads when the environment carries no WSL_DISTRO_NAME.
func noWSLHint(string) ([]byte, error) { return []byte("5.15.0-microsoft-standard-WSL2"), nil }
