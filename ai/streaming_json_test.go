package ai

import (
	"encoding/json"
	"math/rand/v2"
	"os"
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

// D487: ParseStreamingJSONText keeps the preview rules the streaming adapters
// need, which are more tolerant than the reference's parseStreamingJson on
// malformed text. The reference has no caller of that function (grep of
// packages/ai/src finds it in comments only), so the difference is documented
// here rather than split into a second parser.
func TestParseStreamingJSONTextKeepsItsTolerance(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		// A missing comma is tolerated, where the reference drops everything
		// after the gap.
		{`{"a":1 "b":2}`, `{"a":1}`},
		// An invalid escape keeps the text, where the reference gives {}.
		{`{"bad":"\d"`, `{"bad":""}`},
		// A loose number is accepted, where the reference gives {}.
		{`{"p":.5}`, `{"p":0.5}`},
		{`{"plus":+1}`, `{"plus":1}`},
		// An unterminated string keeps its trailing spaces, where the reference
		// trims them.
		{`{"s":"abc   `, `{"s":"abc   "}`},
		// NaN and Infinity prefixes yield {}, where partial-json yields null.
		{`{"n":NaN}`, `{}`},
		// An unterminated key drops the member entirely, where partial-json keeps
		// the empty key it read: `{""k"'` gives `{"":"'"}` there and `{}` here.
		{`{""k"'`, `{}`},
		{`{""}`, `{}`},
		// A complete object is returned as written.
		{`{"ok":1}`, `{"ok":1}`},
	}
	for _, testCase := range cases {
		if got := string(ParseStreamingJSONText(testCase.input)); got != testCase.want {
			t.Errorf("ParseStreamingJSONText(%q) = %s, want %s", testCase.input, got, testCase.want)
		}
	}
}

// Reference: pi v1.5.2 (b4afdddf), packages/ai/src/utils/streaming-json.ts
// ToolCallArgumentsStream. The corpus in testdata/streaming-json is produced by
// running the reference over each text one code point at a time, so this test
// compares the port's incremental parser against the reference rather than
// against expectations written by hand: every preview after every prefix, then
// the final arguments. Only whether a malformed text failed is compared; D221
// records that its message is the Go decoder's.
func TestToolCallArgumentsStreamMatchesReference(t *testing.T) {
	data, err := os.ReadFile("testdata/streaming-json/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Text     string   `json:"text"`
		Previews []string `json:"previews"`
		Final    string   `json:"final"`
		HasError bool     `json:"hasError"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("the streaming-json corpus is empty")
	}
	for _, fixture := range fixtures {
		var args streamingArguments
		previews := fixture.Previews
		if got := string(args.Append("")); got != previews[0] {
			t.Fatalf("%q: empty preview %q, want %q", fixture.Text, got, previews[0])
		}
		for cut, character := range []rune(fixture.Text) {
			want := previews[cut+1]
			if got := string(args.Append(string(character))); got != want {
				t.Fatalf("%q: preview after %d code points is %q, want %q", fixture.Text, cut+1, got, want)
			}
		}
		final, message := args.Finish()
		if string(final) != fixture.Final {
			t.Fatalf("%q: final arguments %q, want %q", fixture.Text, final, fixture.Final)
		}
		if (message != "") != fixture.HasError {
			t.Fatalf("%q: error %q, want hasError %t", fixture.Text, message, fixture.HasError)
		}
	}
}

// While tool-call arguments stream, the UI shows the partial value
// ParseStreamingJSONText produces for the bytes received so far, so every
// prefix has to decode: an unparsable partial would render as broken JSON and
// could reach a caller as arguments. The corpus covers the shapes that break
// hand-written repairs (deep nesting, strings with raw NUL and malformed UTF-8,
// lone surrogate escapes, duplicate keys, huge numbers and a large string).
func TestStreamingJSONPartialIsAlwaysValidJSON(t *testing.T) {
	documents := []string{
		`{"a":1,"b":[1,2,{"c":"x"}],"d":{"e":null}}`,
		`{"path":"/tmp/x","content":"line\nline","n":-1.5e10}`,
		`[1,2,3,{"k":"v"}]`,
		`{"s":"\ud83d\ude00 \u00e9 \t \\ \" end"}`,
		`{"nested":{"deep":{"deeper":[true,false,null,{"x":1}]}}}`,
		`{"":"","k":"","arr":[[],{},[[]]]}`,
		`{"big":1e308,"tiny":1e-308,"zero":0,"neg":-0}`,
		`{"unicode":"漢字👨‍👩‍👧","esc":"\u0000\u001f"}`,
		`{}`,
		`[]`,
		`{"incomplete":`,
		`{"trailing":1,`,
		strings.Repeat(`{"a":`, 500) + `1` + strings.Repeat(`}`, 500),
		"{\"k\":\"\x00\"}",
		"{\"k\":\"\xff\xfe\"}",
		`{"dup":1,"dup":2}`,
		`{"k":"` + strings.Repeat("x", 20000) + `"}`,
	}
	for _, document := range documents {
		step := 1
		if len(document) > 2000 {
			step = 97
		}
		for cut := 0; cut <= len(document); cut += step {
			partial := ParseStreamingJSONText(document[:cut])
			if len(partial) == 0 {
				continue
			}
			if !json.Valid(partial) {
				t.Fatalf("prefix %d of %q emitted invalid JSON %q", cut, document, partial)
			}
			var value any
			if err := json.Unmarshal(partial, &value); err != nil {
				t.Fatalf("prefix %d of %q: %v", cut, document, err)
			}
		}
	}
}
