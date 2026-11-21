// Reference: pi v1.6.0 (faac5858) persists transcripts and sends wire payloads
// through JSON.stringify. This generator records Node's output for a corpus of
// documents, compact and two-space indented, so pier's MarshalJSON and
// MarshalIndentJSON are compared against it. Run from the repository root:
//   node ai/testdata/json-stringify/generate.mjs > ai/testdata/json-stringify/golden.json
const documents = [
  '{}',
  '[]',
  '{"a":1}',
  '{"b":1,"a":2}',
  '{"2":1,"1":2,"b":3,"a":4,"0":5}',
  '{"01":1,"1":2,"1e3":3,"-1":4,"4294967294":5,"4294967295":6}',
  '{"4294967295":1,"4294967294":2}',
  '{"a":{"c":1,"b":[1,2,3]},"z":null}',
  '{"empty":{},"list":[],"nested":[{"x":[[]]}]}',
  '{"s":"héllo 日本 😀"}',
  '{"tab":"a\\tb","nl":"a\\nb","quote":"\\"q\\"","back":"a\\\\b"}',
  '{"ls":"a\\u2028b\\u2029c","literal":"\\\\u2028"}',
  '{"control":"\\u0000\\u001f\\u007f"}',
  '{"n":[0,-0,1,-1,1.5,1e-7,1e21,123456789,0.1,3.14159]}',
  '{"flag":true,"none":null,"neg":false}',
  '{"deep":{"a":{"b":{"c":{"d":[1,[2,[3]]]}}}}}',
  '[[1,2],[3,[4]],{"k":"v"}]',
  '{"emoji":"\\ud83d\\ude00","escaped":"\\ud83d\\ude00"}',
];
// A lone surrogate escape is excluded: JSON.stringify keeps "\ud800" while a Go
// string cannot hold an unpaired surrogate, so pier substitutes U+FFFD (D253).
const cases = documents.map((text) => ({
  input: text,
  compact: JSON.stringify(JSON.parse(text)),
  indented: JSON.stringify(JSON.parse(text), null, 2),
  roundtrip: JSON.stringify(JSON.parse(JSON.stringify(JSON.parse(text)))),
}));
process.stdout.write(`${JSON.stringify(cases, null, '\t')}\n`);
