package coding

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// writeReadFixture writes a file of the given size made of fixed-width lines and
// returns the number of lines it holds.
func writeReadFixture(t *testing.T, path string, size int64) int {
	t.Helper()
	line := []byte(strings.Repeat("x", 62) + "\n")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	chunk := bytes.Repeat(line, 1024)
	var written int64
	for written < size {
		if _, err := file.Write(chunk); err != nil {
			t.Fatal(err)
		}
		written += int64(len(chunk))
	}
	return int(written / int64(len(line)))
}

// pi v1.5.2 (b4afdddf) core/tools/read.ts streams text files and keeps only the
// lines it shows, so reading costs memory in proportion to the output. Before
// that the tool loaded and split the whole file, so one read of a large log held
// several times its size on the heap at once.
func TestReadToolKeepsMemoryBoundedOnALargeFile(t *testing.T) {
	dir := t.TempDir()
	const size = 32 << 20
	lines := writeReadFixture(t, filepath.Join(dir, "big.log"), size)
	params := `{"path":"big.log","limit":10}`
	if _, err := readToolText(t, dir, params); err != nil {
		t.Fatal(err)
	}

	// Sample the live heap while the read runs: the read itself only garbage,
	// so the allocation counters cannot tell the two implementations apart.
	runtime.GC()
	var baseline runtime.MemStats
	runtime.ReadMemStats(&baseline)
	stop := make(chan struct{})
	peak := make(chan uint64, 1)
	go func() {
		high := baseline.HeapAlloc
		for {
			select {
			case <-stop:
				peak <- high
				return
			default:
			}
			var stats runtime.MemStats
			runtime.ReadMemStats(&stats)
			if stats.HeapAlloc > high {
				high = stats.HeapAlloc
			}
			time.Sleep(200 * time.Microsecond)
		}
	}()

	text, err := readToolText(t, dir, params)
	close(stop)
	high := <-peak
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(text, "x") {
		t.Fatalf("output does not start with the first line: %q", text[:min(len(text), 80)])
	}
	// The file ends with a newline, so split("\n") counts one more, empty line.
	if want := fmt.Sprintf("[%d more lines in file. Use offset=11 to continue.]", lines+1-10); !strings.Contains(text, want) {
		t.Fatalf("output = %q; want the remaining-lines notice %q", text, want)
	}

	const budget = 24 << 20
	if growth := high - baseline.HeapAlloc; growth > budget {
		t.Fatalf("reading %d MiB to show 10 lines peaked %d MiB above the baseline heap, want under %d MiB",
			size>>20, growth>>20, budget>>20)
	}
}
