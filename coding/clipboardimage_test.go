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
