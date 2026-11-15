package cmd

import (
	"errors"
	"testing"

	"github.com/dat267/pier/coding"
)

// Reference: pi v1.5.2 (b4afdddf) main.ts prints
// `Session already exists with id '...'` with console.error and no "Error: "
// prefix, like the "No session found matching ..." line. Pier returned a plain
// error, so Execute added the prefix.
func TestForkSessionIDCollisionIsABareLine(t *testing.T) {
	isolatedAgentDir(t)
	cwd := t.TempDir()
	existing := makeSessionFile(t, cwd)
	id := existing.GetSessionID()

	args := &coding.Args{Fork: new(existing.GetSessionFile()), SessionID: &id}
	_, err := resumeSession(args, cwd, nil)
	if err == nil {
		t.Fatal("expected the id collision")
	}
	var bare bareErrorLine
	if !errors.As(err, &bare) {
		t.Fatalf("err %v is not a bare line", err)
	}
	if err.Error() != "Session already exists with id '"+id+"'" {
		t.Fatalf("err = %q", err.Error())
	}
}
