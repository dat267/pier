package ai

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// Frames encoded by @smithy/core's EventStreamCodec, the library the reference
// reads Bedrock ConverseStream with, one per header type plus empty headers,
// an empty payload and a header name long enough for a multi-byte length.
// Regenerate with node ai/testdata/bedrock-eventstream/generate.mjs.
func TestAWSEventStreamDecodesSDKFrames(t *testing.T) {
	data, err := os.ReadFile("testdata/bedrock-eventstream/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name    string `json:"name"`
		Frame   string `json:"frame"`
		Payload string `json:"payload"`
		Headers []struct {
			Name  string          `json:"name"`
			Type  string          `json:"type"`
			Value json.RawMessage `json:"value"`
		} `json:"headers"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("no frames")
	}
	for _, testCase := range cases {
		t.Run(testCase.Name, func(t *testing.T) {
			frame, err := base64.StdEncoding.DecodeString(testCase.Frame)
			if err != nil {
				t.Fatal(err)
			}
			message, err := ReadAWSEventStreamMessage(bytes.NewReader(frame))
			if err != nil {
				t.Fatal(err)
			}
			if len(message.Headers) != len(testCase.Headers) {
				t.Fatalf("headers = %d, want %d", len(message.Headers), len(testCase.Headers))
			}
			for _, header := range testCase.Headers {
				got, ok := message.Headers[header.Name]
				if !ok {
					t.Fatalf("header %q missing", header.Name)
				}
				var want any
				switch header.Type {
				case "string":
					var value string
					if err := json.Unmarshal(header.Value, &value); err != nil {
						t.Fatal(err)
					}
					want = value
				case "boolean":
					var value bool
					if err := json.Unmarshal(header.Value, &value); err != nil {
						t.Fatal(err)
					}
					want = value
				case "byte":
					var value int8
					if err := json.Unmarshal(header.Value, &value); err != nil {
						t.Fatal(err)
					}
					want = value
				case "short":
					var value int16
					if err := json.Unmarshal(header.Value, &value); err != nil {
						t.Fatal(err)
					}
					want = value
				case "integer":
					var value int32
					if err := json.Unmarshal(header.Value, &value); err != nil {
						t.Fatal(err)
					}
					want = value
				case "long", "timestamp":
					var value int64
					if err := json.Unmarshal(header.Value, &value); err != nil {
						t.Fatal(err)
					}
					want = value
				case "binary", "uuid":
					var encoded string
					if err := json.Unmarshal(header.Value, &encoded); err != nil {
						t.Fatal(err)
					}
					decoded, err := base64.StdEncoding.DecodeString(encoded)
					if err != nil {
						t.Fatal(err)
					}
					want = decoded
				default:
					t.Fatalf("unknown golden header type %q", header.Type)
				}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("header %q = %#v, want %#v", header.Name, got, want)
				}
			}
			payload, err := base64.StdEncoding.DecodeString(testCase.Payload)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(message.Payload, payload) {
				t.Fatalf("payload = %q, want %q", message.Payload, payload)
			}
		})
	}
}
