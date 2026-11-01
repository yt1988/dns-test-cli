//go:build windows

package main

import (
	"net/netip"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// systemServers lists the DNS servers of the network adapters that are up, in adapter order.
func systemServers() []netip.Addr {
	size := uint32(15000)
	for try := 0; try < 3; try++ {
		buf := make([]byte, size)
		aa := (*windows.IpAdapterAddresses)(unsafe.Pointer(&buf[0]))
		err := windows.GetAdaptersAddresses(windows.AF_UNSPEC, windows.GAA_FLAG_SKIP_ANYCAST|windows.GAA_FLAG_SKIP_MULTICAST, 0, aa, &size)
		if err == windows.ERROR_BUFFER_OVERFLOW {
			continue
		}
		if err != nil {
			return nil
		}
		var out []netip.Addr
		for a := aa; a != nil; a = a.Next {
			if a.OperStatus != windows.IfOperStatusUp || a.IfType == windows.IF_TYPE_SOFTWARE_LOOPBACK {
				continue
			}
			for d := a.FirstDnsServerAddress; d != nil; d = d.Next {
				sa, err := d.Address.Sockaddr.Sockaddr()
				if err != nil {
					continue
				}
				switch s := sa.(type) {
				case *syscall.SockaddrInet4:
					out = append(out, netip.AddrFrom4(s.Addr))
				case *syscall.SockaddrInet6:
					ip := netip.AddrFrom16(s.Addr)
					// fec0:0:0:ffff::1-3: Windows' placeholder when no IPv6 DNS server is set
					if !netip.MustParsePrefix("fec0::/10").Contains(ip) {
						out = append(out, ip)
					}
				}
			}
		}
		return out
	}
	return nil
}
