package ai

import "testing"

func TestZZNormalizeGitHubDomain(t *testing.T) {
	for _, c := range []string{
		"github.com", "https://github.com", "HTTP://GitHub.COM/x",
		"ftp://github.com", "file:///etc", "javascript:alert(1)",
		"https://github.com:8443", "https://user:pw@github.com",
		" ", "", "not a url", "https://",
	} {
		got, ok := NormalizeGitHubDomain(c)
		t.Logf("RESULT %-28q -> ok=%v %q", c, ok, got)
	}
}
