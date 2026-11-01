//go:build !windows

package main

import (
	"bufio"
	"net/netip"
	"os"
	"strings"
)

// systemServers lists the resolvers this machine is set to use. Behind systemd-resolved's local stub (127.0.0.53),
// the real upstreams are in its own resolv.conf.
func systemServers() []netip.Addr {
	out := readResolvConf("/etc/resolv.conf")
	if len(out) == 1 && out[0].IsLoopback() {
		if up := readResolvConf("/run/systemd/resolve/resolv.conf"); len(up) > 0 {
			return up
		}
	}
	return out
}

func readResolvConf(path string) []netip.Addr {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []netip.Addr
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		if len(fs) >= 2 && fs[0] == "nameserver" {
			if a, err := netip.ParseAddr(strings.SplitN(fs[1], "%", 2)[0]); err == nil {
				out = append(out, a)
			}
		}
	}
	return out
}
