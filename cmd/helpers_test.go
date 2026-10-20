package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dat267/pier/coding"
)

// TestExecutableName derives the display name from the invoked binary, falling
// back to the product name (main.ts's executable name).
func TestExecutableName(t *testing.T) {
	previous := os.Args[0]
	t.Cleanup(func() { os.Args[0] = previous })
	cases := map[string]string{
		"/usr/local/bin/pier":      "pier",
		"/usr/local/bin/pi.exe":    "pi",
		"pier":                     "pier",
		"":                         coding.AppName,
		".":                        coding.AppName,
		string(filepath.Separator): coding.AppName,
	}
	for argument, want := range cases {
		os.Args[0] = argument
		if got := executableName(); got != want {
			t.Fatalf("executableName(%q) = %q, want %q", argument, got, want)
		}
	}
}

// TestReadPipedStdin reads a pipe but skips a character device.
func TestReadPipedStdin(t *testing.T) {
	previous := os.Stdin
	t.Cleanup(func() { os.Stdin = previous })
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	os.Stdin = reader
	if _, err := writer.WriteString("piped prompt"); err != nil {
		t.Fatal(err)
	}
	writer.Close()
	text, piped, err := readPipedStdin()
	if err != nil || !piped || text != "piped prompt" {
		t.Fatalf("readPipedStdin = %q, %v, %v", text, piped, err)
	}
	// A terminal's character device takes the interactive path.
	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer devNull.Close()
	os.Stdin = devNull
	if _, piped, err := readPipedStdin(); err != nil || piped {
		t.Fatalf("character device: piped = %v, %v", piped, err)
	}
}

// Upstream readPipedStdin trims the piped content (`data.trim() || undefined`),
// so a prompt from `echo ... | pi -p` has no trailing newline and a
// whitespace-only pipe contributes nothing.
func TestReadPipedStdinTrimsLikeUpstream(t *testing.T) {
	previous := os.Stdin
	t.Cleanup(func() { os.Stdin = previous })

	read := func(input string) string {
		reader, writer, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		defer reader.Close()
		os.Stdin = reader
		if _, err := writer.WriteString(input); err != nil {
			t.Fatal(err)
		}
		writer.Close()
		text, piped, err := readPipedStdin()
		if err != nil || !piped {
			t.Fatalf("readPipedStdin(%q) = %q, %v, %v", input, text, piped, err)
		}
		return text
	}

	if got := read("  piped prompt\n\n"); got != "piped prompt" {
		t.Fatalf("piped prompt = %q; want the trimmed text", got)
	}
	if got := read(" \n\t\n"); got != "" {
		t.Fatalf("whitespace-only stdin = %q; want empty", got)
	}
}
