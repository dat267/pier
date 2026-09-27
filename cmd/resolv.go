package cmd

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
)

// This file is port-local: upstream runs on Node, whose resolver goes through the
// platform's libc and never reads resolv.conf, so it has no equivalent. Only the
// android build needs it, and it is called once from Execute, before anything can
// touch the network.
//
// Android has no /etc/resolv.conf — /etc is a symlink to the read-only
// /system/etc — and the pure-Go resolver (every build here is CGO_ENABLED=0, so
// netGo is in force) falls back to the loopback defaults, 127.0.0.1:53 and
// [::1]:53, when it cannot read its config. Nothing listens there, so every lookup
// fails with "connection refused". Termux keeps its nameservers in
// $PREFIX/etc/resolv.conf and patches its own Go to read that path, which is why a
// binary built by Termux's Go resolves and a release asset built on a stock
// toolchain does not.

// configureResolver installs a fallback resolver when the platform's resolver
// config is missing.
func configureResolver() {
	path := resolverFallbackPath(runtime.GOOS, stockResolverConfigExists(), os.Getenv)
	if path == "" {
		return
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return
	}
	servers := nameserversFromResolvConf(string(body))
	if len(servers) == 0 {
		return
	}
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: resolverDialer(servers)}
}

// stockResolverConfigExists reports whether the path the pure-Go resolver reads on
// its own is there.
func stockResolverConfigExists() bool {
	_, err := os.Stat("/etc/resolv.conf")
	return err == nil
}

// resolverFallbackPath is the resolv.conf to read when the stock one is absent, or
// "" when the resolver should be left alone.
func resolverFallbackPath(goos string, stockExists bool, getenv func(string) string) string {
	if goos != "android" || stockExists {
		return ""
	}
	if override := strings.TrimSpace(getenv("PIER_RESOLV_CONF")); override != "" {
		return override
	}
	prefix := strings.TrimSpace(getenv("PREFIX"))
	if prefix == "" {
		return ""
	}
	return filepath.Join(prefix, "etc", "resolv.conf")
}

// nameserversFromResolvConf returns the nameserver addresses a resolv.conf lists,
// each as host:port. Comments, blank lines and trailing fields are ignored, and a
// bare address gets the standard port.
func nameserversFromResolvConf(body string) []string {
	var servers []string
	for _, line := range strings.Split(body, "\n") {
		if index := strings.IndexAny(line, "#;"); index >= 0 {
			line = line[:index]
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.EqualFold(fields[0], "nameserver") {
			continue
		}
		address := fields[1]
		if _, _, err := net.SplitHostPort(address); err != nil {
			address = net.JoinHostPort(address, "53")
		}
		servers = append(servers, address)
	}
	return servers
}

// resolverDialer dials the configured nameservers, rotating so a nameserver that
// stops answering is not the only one the resolver ever reaches. The address the
// resolver asks for is ignored: it is one of the loopback defaults that prompted
// the fallback in the first place.
func resolverDialer(servers []string) func(ctx context.Context, network, address string) (net.Conn, error) {
	var next atomic.Uint64
	return func(ctx context.Context, network, _ string) (net.Conn, error) {
		start := int(next.Add(1)-1) % len(servers)
		var lastError error
		for offset := range servers {
			server := servers[(start+offset)%len(servers)]
			conn, err := (&net.Dialer{}).DialContext(ctx, network, server)
			if err == nil {
				return conn, nil
			}
			lastError = err
		}
		return nil, lastError
	}
}
