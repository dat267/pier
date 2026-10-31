package interactive

import (
	"encoding/base64"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/dat267/pier/ai"
)

// pi v1.4.0 (4a080252), core/export-html/index.ts generateHtml inserts a
// base64 JSON payload and the embedded assets into the HTML template. In Go,
// every replacement after that insertion copies the entire payload again.
func TestGenerateSessionHTMLBoundsLargePayloadAllocations(t *testing.T) {
	SetCustomThemesDir(t.TempDir())
	SetRegisteredThemes(nil)
	const size = 2 << 20
	prompt := strings.Repeat("x", size)
	data := &exportSessionData{SystemPrompt: &prompt}
	// Warm type/theme metadata, then measure both operations with cold pools
	// and no automatic GC. JSON allocation costs vary under race instrumentation
	// and are not the template-copy overhead this regression bounds.
	GenerateSessionHTML(data, "dark")
	previousGC := debug.SetGCPercent(-1)
	defer debug.SetGCPercent(previousGC)
	runtime.GC()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	payload, err := ai.MarshalJSON(data)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	serializationBytes := after.TotalAlloc - before.TotalAlloc

	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&before)
	document := GenerateSessionHTML(data, "dark")
	runtime.ReadMemStats(&after)
	allocated := after.TotalAlloc - before.TotalAlloc
	overhead := int64(allocated) - int64(serializationBytes)
	// Allow two base64 buffers, one final document, plus headroom for fixed
	// assets and a race-instrumented encoder pool miss (one 4 MiB buffer).
	// Not three additional payload-sized documents.
	budget := int64(3*base64.StdEncoding.EncodedLen(len(payload)) + 7<<20)
	t.Logf("2 MiB prompt: total=%d, serialization=%d, assembly overhead=%d", allocated, serializationBytes, overhead)
	if overhead > budget {
		t.Errorf("export assembly allocated %d bytes beyond serialization, want at most %d", overhead, budget)
	}

	// The whole document must match a small export with only its embedded
	// session data replaced. Asset insertion must not change payload bytes.
	empty := GenerateSessionHTML(&exportSessionData{}, "dark")
	oldPayload := decodeExportPayload(t, empty)
	want := strings.Replace(empty,
		base64.StdEncoding.EncodeToString([]byte(oldPayload)),
		base64.StdEncoding.EncodeToString(payload), 1)
	if document != want {
		t.Fatal("large export changed document or session payload bytes")
	}
}
