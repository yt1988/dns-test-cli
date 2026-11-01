package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Where the test runs from, as Cloudflare sees it (over the chosen IP family), for the "from" line: city, country and
// network. The address itself is never printed or saved. Same sources as vps2cloud.
type Origin struct {
	ASN     int    `json:"asn,omitempty"`
	Org     string `json:"org,omitempty"`
	City    string `json:"city,omitempty"`
	Country string `json:"country,omitempty"`
}

func origin(v6 bool) *Origin {
	network := "tcp4"
	if v6 {
		network = "tcp6"
	}
	tr := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
		return (&net.Dialer{Timeout: 4 * time.Second}).DialContext(ctx, network, addr)
	}}
	cl := &http.Client{Transport: tr, Timeout: 6 * time.Second}
	get := func(u string) (*http.Response, error) {
		req, _ := http.NewRequest("GET", u, nil)
		req.Header.Set("User-Agent", userAgent())
		req.Header.Set("Referer", "https://speed.cloudflare.com/")
		return cl.Do(req)
	}
	o := &Origin{}
	if res, err := get("https://speed.cloudflare.com/meta"); err == nil {
		var m struct {
			ASN            json.RawMessage `json:"asn"`
			AsOrganization string          `json:"asOrganization"`
			City           string          `json:"city"`
			Country        string          `json:"country"`
		}
		if json.NewDecoder(res.Body).Decode(&m) == nil {
			asn, _ := strconv.Atoi(string(m.ASN))
			o = &Origin{ASN: asn, Org: m.AsOrganization, City: m.City, Country: m.Country}
		}
		res.Body.Close()
	}
	// /meta comes back empty outside a browser on some networks: ident.me names the network
	if o.Org == "" {
		host := "https://v4.ident.me/json"
		if v6 {
			host = "https://v6.ident.me/json"
		}
		if res, err := get(host); err == nil {
			var m struct {
				ASN  json.RawMessage `json:"asn"`
				ASO  string          `json:"aso"`
				CC   string          `json:"cc"`
				City string          `json:"city"`
			}
			if json.NewDecoder(res.Body).Decode(&m) == nil {
				asn, _ := strconv.Atoi(strings.Trim(string(m.ASN), `"`))
				o.ASN, o.Org = asn, m.ASO
				if o.City == "" {
					o.City, o.Country = m.City, m.CC
				}
			}
			res.Body.Close()
		}
	}
	// and the trace still gives the country
	if o.Country == "" {
		if res, err := get("https://speed.cloudflare.com/cdn-cgi/trace"); err == nil {
			b, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
			res.Body.Close()
			for _, ln := range strings.Split(string(b), "\n") {
				if k, v, _ := strings.Cut(ln, "="); k == "loc" {
					o.Country = v
				}
			}
		}
	}
	if o.Org == "" && o.Country == "" {
		return nil
	}
	return o
}

// String: "Frankfurt am Main, DE · AS197540 netcup GmbH"
func (o *Origin) String() string {
	if o == nil {
		return T("unknownNet")
	}
	where := strings.Trim(o.City+", "+o.Country, ", ")
	net := o.Org
	if o.ASN > 0 {
		net = strings.TrimSpace("AS" + strconv.Itoa(o.ASN) + " " + o.Org)
	}
	if where == "" {
		return net
	}
	if net == "" {
		return where
	}
	return where + " · " + net
}
