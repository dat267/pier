package mcp

import (
	"io"
	"strings"
	"testing"
)

// Reference: pi v1.5.2 (b4afdddf) packages/mcp/src/transports/streamable-http.ts
// consumeSseStream: "SSE lines end with CRLF, LF, or a lone CR." A body that
// separates its lines with a bare CR carries a whole event per CR pair, and a
// parser that only splits on LF sees one unparseable line and dispatches
// nothing, so the request waits for a response the server already sent.
func TestConsumeSseStreamAcceptsLoneCRAfterLineTerminators(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []SseEvent
	}{
		{
			name: "data and blank line",
			body: "data: one\r\r",
			want: []SseEvent{{Data: "one"}},
		},
		{
			name: "id before data",
			body: "id: 7\rdata: two\r\r",
			want: []SseEvent{{Data: "two", ID: new("7")}},
		},
		{
			name: "two events",
			body: "data: a\r\rdata: b\r\r",
			want: []SseEvent{{Data: "a"}, {Data: "b"}},
		},
		{
			name: "multi-line data",
			body: "data: a\rdata: b\r\r",
			want: []SseEvent{{Data: "a\nb"}},
		},
		{
			name: "crlf is one terminator",
			body: "data: a\r\ndata: b\r\n\r\n",
			want: []SseEvent{{Data: "a\nb"}},
		},
		{
			name: "mixed terminators",
			body: "data: a\rdata: b\n\r\n",
			want: []SseEvent{{Data: "a\nb"}},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			for _, reader := range []io.Reader{
				strings.NewReader(testCase.body),
				&oneByteReader{data: testCase.body},
			} {
				var events []SseEvent
				err := ConsumeSseStream(reader, ConsumeSseOptions{
					OnEvent: func(event SseEvent) { events = append(events, event) },
				})
				if err != nil {
					t.Fatal(err)
				}
				if len(events) != len(testCase.want) {
					t.Fatalf("events = %+v, want %+v", events, testCase.want)
				}
				for index, want := range testCase.want {
					got := events[index]
					if got.Data != want.Data {
						t.Fatalf("event %d data = %q, want %q", index, got.Data, want.Data)
					}
					if (got.ID == nil) != (want.ID == nil) || (got.ID != nil && *got.ID != *want.ID) {
						t.Fatalf("event %d id = %v, want %v", index, got.ID, want.ID)
					}
				}
			}
		})
	}
}

// oneByteReader delivers one byte per Read, so a terminator split across reads
// is exercised the way a network chunk boundary splits it.
type oneByteReader struct {
	data string
}

func (r *oneByteReader) Read(p []byte) (int, error) {
	if r.data == "" {
		return 0, io.EOF
	}
	p[0] = r.data[0]
	r.data = r.data[1:]
	return 1, nil
}
