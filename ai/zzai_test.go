package ai

import "testing"

func TestZZParseAuthInput(t *testing.T) {
	for _, c := range []string{
		"http://localhost/cb?code=abc&state=xyz",
		"localhost:1234/cb?code=abc",
		"http://localhost/cb?code=abc",
		"anything?code=abc",
		"?code=abc",
		"abc#xyz", "a#b#c", "in#",
		"code=abc&state=xyz",
		"plain-code",
		"https://x/cb?code=a+b&state=s",
		"https://x/cb?code=a%20b",
		"https://x/cb?code=&state=",
		"javascript:alert(1)?code=x",
		"file:///cb?code=f",
		"  https://x/cb?code=ws  ",
	} {
		got := ParseAuthorizationInput(c)
		t.Logf("RESULT %-38q -> code=%q state=%q", c, got.Code, got.State)
	}
}
