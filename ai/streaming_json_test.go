package ai

import (
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"
)

// streamingJSONDoc builds a random JSON document with the cases JSON.parse
// orders or decodes specially: duplicate and array-index keys, escapes,
// surrogate pairs, non-ASCII text, exponents and large numbers.
func streamingJSONDoc(r *rand.Rand, depth int) string {
	atoms := []string{`"a"`, `""`, `"x\ny\t\"q\"\\"`, `"\u0041\u00e9\u2028"`, `"\ud83d\ude00"`, `"é漢😀"`,
		`"a\/b"`, `0`, `-0`, `12`, `-1.5e3`, `1E+2`, `1e400`, `0.1`, `2.50`, `true`, `false`, `null`}
	if depth > 4 || r.IntN(10) < 5 {
		return atoms[r.IntN(len(atoms))]
	}
	ws := func() string { return []string{"", "", " ", "\n\t"}[r.IntN(4)] }
	if r.IntN(2) == 0 {
		var items []string
		for range r.IntN(5) {
			items = append(items, ws()+streamingJSONDoc(r, depth+1)+ws())
		}
		return "[" + strings.Join(items, ",") + "]"
	}
	keys := []string{`"a"`, `"b"`, `"0"`, `"7"`, `"10"`, `"01"`, `"__proto__"`, `"k\u0031"`, `"\ud83d\ude00"`}
	var items []string
	for range r.IntN(6) {
		key := keys[r.IntN(len(keys))]
		if r.IntN(4) == 0 {
			key = `"` + strconv.Itoa(r.IntN(40)) + `"`
		}
		items = append(items, ws()+key+ws()+":"+ws()+streamingJSONDoc(r, depth+1)+ws())
	}
	return "{" + strings.Join(items, ",") + "}"
}

func streamingJSONMutate(r *rand.Rand, doc string) string {
	if doc == "" {
		return doc
	}
	at := r.IntN(len(doc))
	switch r.IntN(5) {
	case 0:
		return doc[:at] + doc[at+1:]
	case 1:
		return doc[:at] + string(",:{}[]\"\\x\x01 "[r.IntN(11)]) + doc[at:]
	case 2:
		return doc[:at]
	case 3:
		return doc + []string{" x", " {}", "\n", "  "}[r.IntN(4)]
	default:
		return doc[:at] + `\ud800` + doc[at:]
	}
}

// Finish returns the strict parse's bytes without re-parsing the text, so it
// must equal ParseToolCallArguments (pi v1.5.0
// packages/ai/src/utils/streaming-json.ts ToolCallArgumentsStream.finish)
// for every input, split anywhere, including inside a UTF-8 sequence.
func TestStreamingArgumentsFinishMatchesFullParse(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for i := range 20000 {
		doc := "{" + strings.TrimPrefix(streamingJSONDoc(r, 1), "[")
		if r.IntN(2) == 0 {
			doc = streamingJSONDoc(r, 0)
		}
		if r.IntN(3) == 0 {
			doc = streamingJSONMutate(r, doc)
		}
		var args streamingArguments
		for rest := doc; rest != ""; {
			n := min(len(rest), 1+r.IntN(8))
			args.Append(rest[:n])
			rest = rest[n:]
		}
		got, gotErr := args.Finish()
		want, wantErr := ParseToolCallArguments(doc)
		if string(got) != string(want) || gotErr != wantErr {
			t.Fatalf("case %d %q:\n got %s %q\nwant %s %q", i, doc, got, gotErr, want, wantErr)
		}
	}
}

// sjSpecialWord must flag exactly the words holding a byte that a JSON
// string run cannot copy as is.
func TestStreamingJSONSpecialWordMatchesByteScan(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	for range 200000 {
		var b [8]byte
		want := false
		for i := range b {
			b[i] = byte(r.IntN(256))
			if r.IntN(2) == 0 {
				b[i] = byte(0x20 + r.IntN(0x60))
			}
			want = want || b[i] < 0x20 || b[i] >= 0x80 || b[i] == '"' || b[i] == '\\'
		}
		if got := sjSpecialWord(loadWord(string(b[:]))); got != want {
			t.Fatalf("% x: got %v, want %v", b, got, want)
		}
	}
}

// streamingJSONFinal streams text in pieces of size bytes and returns the last
// preview, with the frame gate always open.
func streamingJSONFinal(t *testing.T, text string, size int) string {
	t.Helper()
	fakeStreamingJSONClock(t, 1<<40)
	var args streamingArguments
	var last []byte
	for rest := text; rest != ""; {
		n := min(size, len(rest))
		last = args.Append(rest[:n])
		rest = rest[n:]
	}
	return string(last)
}

// fakeStreamingJSONClock advances the preview clock by step per reading.
func fakeStreamingJSONClock(t *testing.T, step int64) {
	t.Helper()
	saved := sjNow
	clock := int64(0)
	sjNow = func() int64 { clock += step; return clock }
	t.Cleanup(func() { sjNow = saved })
}

// Cases from pi v1.5.0 packages/ai/test/streaming-json.test.ts.
func TestStreamingJSONUpstreamCases(t *testing.T) {
	for _, tc := range []struct{ text, want string }{
		// holds an escape back until it is complete
		{`{"a":"x\u00e9y"}`, `{"a":"xéy"}`},
		{`{"a":"x\ny"}`, `{"a":"x\ny"}`},
		{`{"a":"x\\y"}`, `{"a":"x\\y"}`},
		{`{"a":"x\"y"}`, `{"a":"x\"y"}`},
		{`{"a":"x\/y"}`, `{"a":"x/y"}`},
		// joins surrogate pairs split across pieces, escaped or raw
		{`{"a":"\ud83d\ude00!"}`, `{"a":"😀!"}`},
		{`{"a":"😀!"}`, `{"a":"😀!"}`},
		// a high surrogate whose low half has not arrived shows alone
		{`{"a":"\ud83d`, `{"a":"\ud83d"}`},
		// keeps trailing whitespace of an unterminated string
		{"{\"code\":\"x\t\t  ", `{"code":"x\t\t  "}`},
		// keeps raw control characters and invalid escapes visible while streaming
		{"{\"a\":\"x\ny \\d \\u12\"", `{"a":"x\ny \\d \\u12"}`},
		// keeps invalid \u escapes literal without dropping fields (#10868)
		{`{"path":"a.tex","content":"\usepackage{x}","mode":"w"}`, `{"path":"a.tex","content":"\\usepackage{x}","mode":"w"}`},
		{`{"text":"caf\u00e9 \url"}`, `{"text":"café \\url"}`},
		{`{"text":"ab\u00`, `{"text":"ab"}`},
		// makes __proto__ an own member; duplicate keys keep the first position
		{`{"__proto__":{"polluted":true},"b":{"__proto__":"x"}}`, `{"__proto__":{"polluted":true},"b":{"__proto__":"x"}}`},
		{`{"a":1,"b":2,"a":"three"}`, `{"a":"three","b":2}`},
		// tolerates missing commas and colons and drops values in invalid positions
		{`{"a":1 "b":2}`, `{"a":1,"b":2}`},
		{`{"a" 1, "b": 2}`, `{"a":1,"b":2}`},
		{`{"a":[1 2, , 3,]}`, `{"a":[1,2,3]}`},
		{`{"a":NaN,"b":Infinity,"e":1}`, `{"e":1}`},
		{`{"a":"x"} trailing garbage {"b":2}`, `{"a":"x"}`},
		// array-index keys enumerate first, as JSON.parse orders them
		{`{"b":1,"10":2,"2":3,"a":4}`, `{"2":3,"10":2,"b":1,"a":4}`},
		// shows non-object roots as no arguments yet
		{`[1,2`, `{}`},
		{`  `, `{}`},
	} {
		for _, size := range []int{1, 2, 3, 7, len(tc.text)} {
			if got := streamingJSONFinal(t, tc.text, max(size, 1)); got != tc.want {
				t.Errorf("%q in %d-byte pieces: got %s, want %s", tc.text, size, got, tc.want)
			}
		}
	}
}

// ToolCallArgumentsStream.append returns the same arguments while a piece
// changes nothing, and finish is the one full parse (pi v1.5.0
// streaming-json.test.ts "shows partial arguments as an object").
func TestStreamingArgumentsUnchangedPieceKeepsPreview(t *testing.T) {
	var args streamingArguments
	if got := string(args.Append("  ")); got != `{}` {
		t.Fatalf("whitespace preview = %s", got)
	}
	if got := string(args.Append(`{"path":"a.ts","content":"x`)); got != `{"path":"a.ts","content":"x"}` {
		t.Fatalf("preview = %s", got)
	}
	before := args.Append("y")
	if after := args.Append(""); &after[0] != &before[0] {
		t.Fatal("an empty piece replaced the preview")
	}
	got, errText := args.Finish()
	want, wantErr := ParseToolCallArguments(`  {"path":"a.ts","content":"xy`)
	if string(got) != string(want) || errText != wantErr {
		t.Fatalf("unterminated finish = %s %q, want %s %q", got, errText, want, wantErr)
	}
	args.Append(`"}`)
	if got, errText := args.Finish(); string(got) != `{"path":"a.ts","content":"xy"}` || errText != "" {
		t.Fatalf("finish = %s %q", got, errText)
	}
}
