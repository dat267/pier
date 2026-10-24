package ai

import "testing"

// Reference: pi v1.4.0 (4a080252), api/google-generative-ai.ts catch with the
// @google/genai throwErrorIfNotOK message composition. A JSON body is
// JSON.parse/JSON.stringify'd; any other content type is wrapped in the SDK's
// error envelope. D269 records the unparseable-JSON wording difference.
func TestGoogleAPIErrorMessage(t *testing.T) {
	cases := []struct {
		name        string
		status      int
		statusLine  string
		contentType string
		body        string
		want        string
	}{
		{
			name: "json body is re-stringified", status: 401, statusLine: "401 Unauthorized",
			contentType: "application/json",
			body:        `{"error":{"code":401,"message":"API key not valid"}}`,
			want:        `{"error":{"code":401,"message":"API key not valid"}}`,
		},
		{
			name: "json member order and compacting", status: 403, statusLine: "403 Forbidden",
			contentType: "application/json; charset=utf-8",
			body:        "{\n  \"z\": 1,\n  \"a\": {\"y\": 2.0, \"b\": 3}\n}",
			want:        `{"z":1,"a":{"y":2,"b":3}}`,
		},
		{
			name: "text body is wrapped", status: 403, statusLine: "403 Forbidden",
			contentType: "text/plain", body: "nope",
			want: `{"error":{"message":"nope","code":403,"status":"Forbidden"}}`,
		},
		{
			name: "html body is escaped like JavaScript", status: 502, statusLine: "502 Bad Gateway",
			contentType: "text/html", body: "<html>bad gateway</html>",
			want: `{"error":{"message":"<html>bad gateway</html>","code":502,"status":"Bad Gateway"}}`,
		},
		{
			// D269: upstream surfaces V8's JSON.parse SyntaxError here.
			name: "unparseable json keeps decoder wording", status: 403, statusLine: "403 Forbidden",
			contentType: "application/json", body: "nope",
			want: "invalid character 'o' in literal null (expecting 'u')",
		},
		{
			// D269: upstream reports "Unexpected end of JSON input".
			name: "empty json keeps decoder wording", status: 400, statusLine: "400 Bad Request",
			contentType: "application/json", body: "",
			want: "unexpected end of JSON input",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := googleAPIErrorMessage(tc.status, tc.statusLine, tc.contentType, []byte(tc.body))
			if got != tc.want {
				t.Fatalf("message = %q\nwant      %q", got, tc.want)
			}
		})
	}
}
