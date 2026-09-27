package cmd

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// A resolv.conf as Termux writes it, plus the shapes a real file has: comments,
// blank lines, trailing fields, a bracketed IPv6 address, and a bare one.
func TestNameserversFromResolvConf(t *testing.T) {
	body := strings.Join([]string{
		"# generated",
		"",
		"nameserver 8.8.8.8",
		"nameserver 1.1.1.1 ; fallback",
		"NAMESERVER 2001:4860:4860::8888",
		"nameserver [2606:4700:4700::1111]:53",
		"nameserver",
		"search example.com",
		"options timeout:2",
		"nameserver 9.9.9.9 extra fields are ignored",
	}, "\n")

	got := nameserversFromResolvConf(body)
	want := []string{"8.8.8.8:53", "1.1.1.1:53", "[2001:4860:4860::8888]:53", "[2606:4700:4700::1111]:53", "9.9.9.9:53"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("servers = %v, want %v", got, want)
	}
}

func TestResolverFallbackPath(t *testing.T) {
	const prefix = "/data/data/com.termux/files/usr"
	env := func(values map[string]string) func(string) string {
		return func(key string) string { return values[key] }
	}

	cases := []struct {
		name        string
		goos        string
		stockExists bool
		values      map[string]string
		want        string
	}{
		{"android without the stock config", "android", false, map[string]string{"PREFIX": prefix}, filepath.Join(prefix, "etc", "resolv.conf")},
		{"android with the stock config", "android", true, map[string]string{"PREFIX": prefix}, ""},
		{"android without PREFIX", "android", false, map[string]string{}, ""},
		{"the override wins", "android", false, map[string]string{"PREFIX": prefix, "PIER_RESOLV_CONF": "/tmp/other.conf"}, "/tmp/other.conf"},
		{"the override is trimmed", "android", false, map[string]string{"PIER_RESOLV_CONF": "  /tmp/padded.conf  "}, "/tmp/padded.conf"},
		{"other platforms are left alone", "linux", false, map[string]string{"PREFIX": prefix}, ""},
		{"PREFIX is trimmed", "android", false, map[string]string{"PREFIX": " " + prefix + " "}, filepath.Join(prefix, "etc", "resolv.conf")},
	}
	for _, testCase := range cases {
		got := resolverFallbackPath(testCase.goos, testCase.stockExists, env(testCase.values))
		if got != testCase.want {
			t.Errorf("%s: path = %q, want %q", testCase.name, got, testCase.want)
		}
	}
}

// UDP needs no listener, so a dial always succeeds and the address it returns shows
// which nameserver was picked: the rotation is what keeps a nameserver that stops
// answering from being the only one tried.
func TestResolverDialerRotatesServers(t *testing.T) {
	servers := []string{"127.0.0.1:5301", "127.0.0.1:5302"}
	dial := resolverDialer(servers)

	var seen []string
	for range 4 {
		conn, err := dial(context.Background(), "udp", "127.0.0.1:53")
		if err != nil {
			t.Fatal(err)
		}
		seen = append(seen, conn.RemoteAddr().String())
		_ = conn.Close()
	}
	want := []string{servers[0], servers[1], servers[0], servers[1]}
	if strings.Join(seen, " ") != strings.Join(want, " ") {
		t.Fatalf("dialed %v, want %v", seen, want)
	}
}

// The resolver the fallback installs must target the configured nameserver, and a
// missing config must leave the default resolver untouched.
func TestConfigureResolverInstallsAFallback(t *testing.T) {
	if runtime.GOOS != "android" || stockResolverConfigExists() {
		t.Skip("only meaningful where the stock config is missing")
	}
	previous := net.DefaultResolver
	t.Cleanup(func() { net.DefaultResolver = previous })

	path := filepath.Join(t.TempDir(), "resolv.conf")
	if err := os.WriteFile(path, []byte("nameserver 127.0.0.1:5321\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PIER_RESOLV_CONF", path)
	configureResolver()
	if net.DefaultResolver == previous {
		t.Fatal("the fallback resolver was not installed")
	}
	conn, err := net.DefaultResolver.Dial(context.Background(), "udp", "127.0.0.1:53")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if got := conn.RemoteAddr().String(); got != "127.0.0.1:5321" {
		t.Fatalf("the fallback dialed %q", got)
	}

	// No usable config means no replacement.
	net.DefaultResolver = previous
	t.Setenv("PIER_RESOLV_CONF", filepath.Join(t.TempDir(), "absent.conf"))
	configureResolver()
	if net.DefaultResolver != previous {
		t.Fatal("the resolver was replaced without a config to read")
	}
}
