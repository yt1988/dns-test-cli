package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// The resolver list is dns.1ms.dog's (built from src/resolvers.mjs in github.com/yt1988/dns-test). Each run fetches
// the current copy first, so a resolver added there reaches every installed binary; resolvers.json here is the copy
// built into the binary for offline use, refreshed daily by .github/workflows/resolvers.yml.

//go:embed resolvers.json
var builtinList []byte

const listURL = "https://dns.1ms.dog/resolvers.json"

type endpoint struct {
	Host string   `json:"host"` // TLS name (DoT, DoQ)
	URL  string   `json:"url"`  // DoH
	IPs  []string `json:"ips"`  // addresses that serve it, first is used; empty: look the host up
	IPs6 []string `json:"ips6"`
}

type resolver struct {
	ID    string    `json:"id"`
	Name  string    `json:"name"`
	Group string    `json:"group"` // global, cn, region
	CC    string    `json:"cc"`
	Site  string    `json:"site"`
	UDP   []string  `json:"udp"` // plain DNS, UDP and TCP
	UDP6  []string  `json:"udp6"`
	DoT   *endpoint `json:"dot"`
	DoH   *endpoint `json:"doh"`
	DoQ   *endpoint `json:"doq"`
}

type checks struct {
	Bad  string `json:"dnssec_bad"`  // a deliberately broken DNSSEC signature: a validating resolver says SERVFAIL
	Good string `json:"dnssec_good"` // a signed name: a validating resolver sets AD
	ECS  string `json:"ecs"`         // TXT: the resolver's outgoing address, and the client subnet it passed on, if any
}

type list struct {
	Version   int        `json:"version"`
	Names     []string   `json:"names"`  // popular names, cached at any busy resolver
	NXZone    string     `json:"nxzone"` // random names under it exist nowhere: never cached, always NXDOMAIN
	Checks    checks     `json:"checks"`
	Resolvers []resolver `json:"resolvers"`
}

func parseList(b []byte) (*list, error) {
	var l list
	if err := json.Unmarshal(b, &l); err != nil {
		return nil, err
	}
	if l.Version != 1 || len(l.Resolvers) == 0 || len(l.Names) == 0 || l.NXZone == "" {
		return nil, fmt.Errorf("not a resolver list this version understands")
	}
	return &l, nil
}

// loadList: a local file, else the site's current list, else the built-in one.
func loadList(file string, offline bool) (*list, string, error) {
	if file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			return nil, "", err
		}
		l, err := parseList(b)
		return l, file, err
	}
	if !offline {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, "GET", listURL, nil)
		req.Header.Set("User-Agent", "dnstest/"+version)
		if res, err := http.DefaultClient.Do(req); err == nil {
			b, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
			res.Body.Close()
			if err == nil && res.StatusCode == 200 {
				if l, err := parseList(b); err == nil {
					return l, "online", nil
				}
			}
		}
	}
	l, err := parseList(builtinList)
	return l, "built-in", err
}
