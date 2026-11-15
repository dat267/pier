// Reference: pi v1.5.2 (b4afdddf) reads Bedrock ConverseStream through the AWS
// SDK's event-stream codec (@smithy/core eventstream-codec). This generator
// encodes frames with that codec, one per header type, so the port's decoder in
// ai/aws_eventstream.go is compared against the library the reference uses.
// Run from the repository root:
//   node ai/testdata/bedrock-eventstream/generate.mjs > ai/testdata/bedrock-eventstream/golden.json
import {createRequire} from 'node:module';
const require = createRequire(new URL('../../../pi/package.json', import.meta.url));
const {EventStreamCodec, Int64} = require('@smithy/core/event-streams');

const codec = new EventStreamCodec(
  (bytes) => Buffer.from(bytes).toString('utf8'),
  (text) => new Uint8Array(Buffer.from(text, 'utf8')),
);
const utf8 = new TextEncoder();

function frame(headers, body) {
  const encoded = codec.encode({headers, body: utf8.encode(body)});
  return Buffer.from(encoded).toString('base64');
}

function stringHeader(value) { return {type: 'string', value}; }
function boolHeader(value) { return {type: 'boolean', value}; }
function byteHeader(value) { return {type: 'byte', value}; }
function shortHeader(value) { return {type: 'short', value}; }
function intHeader(value) { return {type: 'integer', value}; }
function longHeader(value) { return {type: 'long', value: Int64.fromNumber(value)}; }
function timestampHeader(value) { return {type: 'timestamp', value}; }
function bytesHeader(value) { return {type: 'binary', value: new Uint8Array(Buffer.from(value, 'base64'))}; }
function uuidHeader(value) { return {type: 'uuid', value}; }

const cases = [
  {
    name: 'bedrock-event',
    headers: {
      ':message-type': stringHeader('event'),
      ':event-type': stringHeader('contentBlockDelta'),
      ':content-type': stringHeader('application/json'),
      ':event-signature': bytesHeader('AAECAwQ='),
    },
    body: '{"delta":{"text":"hi"}}',
  },
  {
    name: 'all-header-types',
    headers: {
      boolTrue: boolHeader(true),
      boolFalse: boolHeader(false),
      byteValue: byteHeader(-7),
      shortValue: shortHeader(-300),
      intValue: intHeader(123456),
      longValue: longHeader(1099511627776),
      timestampValue: timestampHeader(1700000000000),
      bytesValue: bytesHeader('/wCA'),
      stringValue: stringHeader('需要 escaping: ünïcode'),
      uuidValue: uuidHeader('8f4d2c3b-1a0f-4e5d-9c8b-7a6958473625'),
    },
    body: '{"a":1}',
  },
  {
    name: 'empty-headers',
    headers: {},
    body: 'plain',
  },
  {
    name: 'empty-payload',
    headers: {':message-type': stringHeader('exception'), ':exception-type': stringHeader('ThrottlingException')},
    body: '',
  },
  {
    name: 'long-header-name',
    headers: {[`x-${'a'.repeat(120)}`]: stringHeader('v')},
    body: '{}',
  },
];

const out = cases.map((entry) => ({
  name: entry.name,
  frame: frame(entry.headers, entry.body),
  headers: Object.entries(entry.headers).map(([name, header]) => ({
    name,
    type: header.type,
    value: header.value instanceof Uint8Array ? Buffer.from(header.value).toString('base64') :
      header.type === 'uuid' ? Buffer.from(header.value.replace(/-/g, ''), 'hex').toString('base64') :
      header.type === 'long' ? Number(BigInt(`0x${Buffer.from(header.value.bytes).toString('hex')}`)) : header.value,
  })),
  payload: Buffer.from(utf8.encode(entry.body)).toString('base64'),
}));
process.stdout.write(`${JSON.stringify(out, null, '\t')}\n`);
