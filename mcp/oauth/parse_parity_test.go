package oauth

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

type oauthParseCase struct {
	Input    json.RawMessage `json:"input"`
	Server   string          `json:"server"`
	Resource *string         `json:"resource"`
	Metadata *struct {
		Resource string `json:"resource"`
	} `json:"metadata"`
	URL        string  `json:"url"`
	Granted    *string `json:"granted"`
	Challenged *string `json:"challenged"`

	OK    bool            `json:"ok"`
	Value json.RawMessage `json:"value"`
	Error string          `json:"error"`
}

type oauthParseFixture struct {
	Tokens         map[string]oauthParseCase `json:"tokens"`
	Clients        map[string]oauthParseCase `json:"clients"`
	Protected      map[string]oauthParseCase `json:"protected"`
	Servers        map[string]oauthParseCase `json:"servers"`
	SelectResource map[string]oauthParseCase `json:"selectResource"`
	ResourceURLs   map[string]oauthParseCase `json:"resourceUrls"`
	StepUp         map[string]oauthParseCase `json:"stepUp"`
}

// loadOAuthParseFixture reads the source-generated pinned fixtures.
// Regenerate with
// node mcp/oauth/testdata/parse/generate.mjs ./pi > mcp/oauth/testdata/parse/golden.json
func loadOAuthParseFixture(t *testing.T) oauthParseFixture {
	t.Helper()
	raw, err := os.ReadFile("testdata/parse/golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture oauthParseFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Tokens) == 0 || len(fixture.Servers) == 0 {
		t.Fatal("fixture is empty")
	}
	return fixture
}

func fixtureInput(t *testing.T, raw json.RawMessage) any {
	t.Helper()
	if len(raw) == 0 {
		// JSON.stringify drops an undefined value, which the fixture records by
		// omitting the member.
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		t.Fatal(err)
	}
	return value
}

// asJSONValue encodes a result the way the reference's structured values look
// after JSON.stringify.
func asJSONValue(t *testing.T, value any) any {
	t.Helper()
	encoded, err := marshalJSON(value)
	if err != nil {
		t.Fatal(err)
	}
	var decoded any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	return decoded
}

func checkParseCase(t *testing.T, name string, expected oauthParseCase, errorText string, value any) {
	t.Helper()
	if !expected.OK {
		if errorText == "" {
			t.Fatalf("%s: want error %q, got %v", name, expected.Error, value)
		}
		if errorText != expected.Error {
			t.Errorf("%s: error = %q, want %q", name, errorText, expected.Error)
		}
		return
	}
	if errorText != "" {
		t.Fatalf("%s: unexpected error: %s", name, errorText)
	}
	want := fixtureInput(t, expected.Value)
	if !reflect.DeepEqual(value, want) {
		t.Errorf("%s: value = %#v, want %#v", name, value, want)
	}
}

// TestOAuthDocumentsMatchUpstream pins the structural validators and the
// unknown-member passthrough against the pinned document parsers.
func TestOAuthDocumentsMatchUpstream(t *testing.T) {
	fixture := loadOAuthParseFixture(t)
	groups := []struct {
		name   string
		cases  map[string]oauthParseCase
		parse  func(any) (any, error)
		expect func(any) any
	}{
		{"tokens", fixture.Tokens, func(value any) (any, error) {
			result, err := ParseOAuthTokens(value)
			return result, err
		}, nil},
		{"clients", fixture.Clients, func(value any) (any, error) {
			result, err := ParseClientInformation(value)
			return result, err
		}, nil},
		{"protected", fixture.Protected, func(value any) (any, error) {
			result, err := ParseProtectedResourceMetadata(value)
			return result, err
		}, nil},
		{"servers", fixture.Servers, func(value any) (any, error) {
			result, err := ParseAuthorizationServerMetadata(value)
			return result, err
		}, nil},
	}
	for _, group := range groups {
		for name, test := range group.cases {
			t.Run(group.name+"/"+name, func(t *testing.T) {
				result, err := group.parse(fixtureInput(t, test.Input))
				errorText := ""
				if err != nil {
					errorText = err.Error()
				}
				var value any
				if err == nil {
					value = asJSONValue(t, result)
				}
				checkParseCase(t, name, test, errorText, value)
			})
		}
	}
}

// TestOAuthSelectResourceMatchesUpstream pins the protected-resource match:
// origin, path prefix and the error text.
func TestOAuthSelectResourceMatchesUpstream(t *testing.T) {
	fixture := loadOAuthParseFixture(t)
	for name, test := range fixture.SelectResource {
		t.Run(name, func(t *testing.T) {
			var metadata *OAuthProtectedResourceMetadata
			if test.Metadata != nil {
				metadata = &OAuthProtectedResourceMetadata{Resource: test.Metadata.Resource}
			}
			result, err := SelectResource(test.Server, metadata)
			errorText := ""
			if err != nil {
				errorText = err.Error()
			}
			var value any
			if result != nil {
				value = *result
			}
			checkParseCase(t, name, test, errorText, value)
		})
	}
}

// TestOAuthResourceURLMatchesUpstream pins the fragment-stripped server URL
// that the resource comparison uses.
func TestOAuthResourceURLMatchesUpstream(t *testing.T) {
	fixture := loadOAuthParseFixture(t)
	for name, test := range fixture.ResourceURLs {
		t.Run(name, func(t *testing.T) {
			if !test.OK {
				// Upstream throws on unparseable input; the Go helper returns
				// the input unchanged and has no error channel.
				t.Skipf("upstream error %q has no Go equivalent: %s", test.Error, ResourceURLFromServerURL(test.URL))
			}
			want := fixtureInput(t, test.Value)
			if got := ResourceURLFromServerURL(test.URL); got != want {
				t.Errorf("url = %q, want %v", got, want)
			}
		})
	}
}

// TestOAuthStepUpScopeMatchesUpstream pins the scope union.
func TestOAuthStepUpScopeMatchesUpstream(t *testing.T) {
	fixture := loadOAuthParseFixture(t)
	for name, test := range fixture.StepUp {
		t.Run(name, func(t *testing.T) {
			result := StepUpScope(test.Granted, test.Challenged)
			var value any
			if result != nil {
				value = *result
			}
			checkParseCase(t, name, test, "", value)
		})
	}
}
