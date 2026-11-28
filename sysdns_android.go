//go:build android

package main

import (
	"net/netip"
	"os"
	"strings"
)

// systemServers receives the DNS servers reported by Android ConnectivityManager.
// The app passes them over JNI because Android does not expose resolv.conf to apps.
func systemServers() []netip.Addr {
	var out []netip.Addr
	seen := map[netip.Addr]bool{}
	for _, raw := range strings.Split(os.Getenv("DNSTEST_SYSTEM_DNS"), ",") {
		raw = strings.TrimSpace(strings.SplitN(raw, "%", 2)[0])
		if raw == "" {
			continue
		}
		addr, err := netip.ParseAddr(raw)
		if err != nil || seen[addr] {
			continue
		}
		seen[addr] = true
		out = append(out, addr)
	}
	return out
}
