package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
)

// The five ways to ask a resolver. Every client keeps its connection open between queries, and the time it reports
// for a query runs from sending it to having the whole reply: opening a connection (TCP, TLS or QUIC handshake) is
// timed apart, never inside a query. A query on a connection the server had meanwhile closed is sent again on a new
// one, and only that second exchange is timed.
const (
	pUDP = "udp"
	pTCP = "tcp"
	pDoT = "dot"
	pDoH = "doh"
	pDoQ = "doq"
)

var protos = []string{pUDP, pTCP, pDoT, pDoH, pDoQ}

type client interface {
	// connect opens the connection and reports how long the handshake took (0 for UDP).
	connect(ctx context.Context) (time.Duration, error)
	// exchange sends query q and returns the reply and its time.
	exchange(ctx context.Context, q []byte) ([]byte, time.Duration, error)
	close()
}

var dialer = net.Dialer{KeepAlive: -1}

func tlsConf(host string, alpn ...string) *tls.Config {
	return &tls.Config{ServerName: host, NextProtos: alpn, MinVersion: tls.VersionTLS12}
}

/* ---------- UDP ---------- */

type udpClient struct {
	addr string
	mu   sync.Mutex
	conn net.Conn
}

func (c *udpClient) connect(ctx context.Context) (time.Duration, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		return 0, nil
	}
	conn, err := dialer.DialContext(ctx, "udp", c.addr)
	if err != nil {
		return 0, err
	}
	c.conn = conn
	return 0, nil
}

func (c *udpClient) exchange(ctx context.Context, q []byte) ([]byte, time.Duration, error) {
	if _, err := c.connect(ctx); err != nil {
		return nil, 0, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if d, ok := ctx.Deadline(); ok {
		c.conn.SetDeadline(d)
	}
	t0 := time.Now()
	if _, err := c.conn.Write(q); err != nil {
		return nil, 0, err
	}
	buf := make([]byte, 4096)
	for {
		n, err := c.conn.Read(buf)
		if err != nil {
			return nil, 0, err
		}
		// a late reply to an earlier query that timed out: keep waiting for this one
		if n >= 2 && msgID(buf[:n]) == msgID(q) {
			return append([]byte(nil), buf[:n]...), time.Since(t0), nil
		}
	}
}

func (c *udpClient) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
}

/* ---------- TCP and DoT: two-byte length, then the message (RFC 1035 4.2.2, RFC 7858) ---------- */

type streamClient struct {
	addr, host string // host: the TLS name for DoT; empty for plain TCP
	mu         sync.Mutex
	conn       net.Conn
}

func (c *streamClient) dial(ctx context.Context) (net.Conn, time.Duration, error) {
	t0 := time.Now()
	conn, err := dialer.DialContext(ctx, "tcp", c.addr)
	if err != nil {
		return nil, 0, err
	}
	if c.host != "" {
		tc := tls.Client(conn, tlsConf(c.host, "dot"))
		if err := tc.HandshakeContext(ctx); err != nil {
			conn.Close()
			return nil, 0, err
		}
		conn = tc
	}
	return conn, time.Since(t0), nil
}

func (c *streamClient) connect(ctx context.Context) (time.Duration, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
	conn, d, err := c.dial(ctx)
	if err != nil {
		return 0, err
	}
	c.conn = conn
	return d, nil
}

func writeFramed(w io.Writer, q []byte) error {
	b := make([]byte, 2+len(q))
	binary.BigEndian.PutUint16(b, uint16(len(q)))
	copy(b[2:], q)
	_, err := w.Write(b)
	return err
}

func readFramed(r io.Reader) ([]byte, error) {
	var l [2]byte
	if _, err := io.ReadFull(r, l[:]); err != nil {
		return nil, err
	}
	m := make([]byte, binary.BigEndian.Uint16(l[:]))
	_, err := io.ReadFull(r, m)
	return m, err
}

func (c *streamClient) exchange(ctx context.Context, q []byte) ([]byte, time.Duration, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for try := 0; ; try++ {
		reused := c.conn != nil
		if !reused {
			conn, _, err := c.dial(ctx)
			if err != nil {
				return nil, 0, err
			}
			c.conn = conn
		}
		if d, ok := ctx.Deadline(); ok {
			c.conn.SetDeadline(d)
		}
		t0 := time.Now()
		err := writeFramed(c.conn, q)
		var m []byte
		for err == nil {
			// replies on one connection may come out of order (RFC 7766): skip any for other queries
			if m, err = readFramed(c.conn); err == nil && msgID(m) == msgID(q) {
				return m, time.Since(t0), nil
			}
		}
		c.conn.Close()
		c.conn = nil
		// the server closed an idle connection: once more on a new one; a timeout is a timeout
		if !reused || try > 0 || ctx.Err() != nil || isTimeout(err) {
			return nil, 0, err
		}
	}
}

func (c *streamClient) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

/* ---------- DoH: POST application/dns-message over HTTP/2 (RFC 8484) ---------- */

type dohClient struct {
	url, addr, host string
	hc              *http.Client
}

func newDoH(url, host, addr string) *dohClient {
	tr := &http.Transport{
		// every request goes to the chosen address, whatever the system resolver says about the host
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, addr)
		},
		TLSClientConfig:     tlsConf(host),
		ForceAttemptHTTP2:   true,
		MaxIdleConnsPerHost: 1,
		IdleConnTimeout:     5 * time.Minute,
		DisableCompression:  true,
		Proxy:               nil, // measure the network, not a proxy set in the environment
	}
	return &dohClient{url: url, addr: addr, host: host, hc: &http.Client{Transport: tr}}
}

// do sends one request. ready is when the connection was there to send on (after any handshake), so the time from
// it to the end of the body is the query alone; hs is the handshake when this request had to open the connection.
func (c *dohClient) do(ctx context.Context, q []byte) (body []byte, took, hs time.Duration, err error) {
	var t0, connStart, ready time.Time
	var fresh bool
	trace := &httptrace.ClientTrace{
		ConnectStart: func(string, string) { connStart = time.Now() },
		GotConn: func(i httptrace.GotConnInfo) {
			ready = time.Now()
			fresh = !i.Reused
		},
	}
	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), "POST", c.url, bytes.NewReader(q))
	if err != nil {
		return nil, 0, 0, err
	}
	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")
	t0 = time.Now()
	res, err := c.hc.Do(req)
	if err != nil {
		return nil, 0, 0, err
	}
	defer res.Body.Close()
	body, err = io.ReadAll(io.LimitReader(res.Body, 65536))
	end := time.Now()
	if err != nil {
		return nil, 0, 0, err
	}
	if res.StatusCode != 200 {
		return nil, 0, 0, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	if ready.IsZero() {
		ready = t0
	}
	if fresh && !connStart.IsZero() {
		hs = ready.Sub(connStart)
	}
	return body, end.Sub(ready), hs, nil
}

func (c *dohClient) connect(ctx context.Context) (time.Duration, error) {
	// the first request opens the connection; its handshake is the answer
	c.hc.CloseIdleConnections()
	q, _ := newQuery("dns.google", 1, 0)
	_, _, hs, err := c.do(ctx, q)
	return hs, err
}

func (c *dohClient) exchange(ctx context.Context, q []byte) ([]byte, time.Duration, error) {
	b, took, _, err := c.do(ctx, q)
	return b, took, err
}

func (c *dohClient) close() { c.hc.CloseIdleConnections() }

/* ---------- DoQ: one QUIC stream per query, two-byte length, ID 0 (RFC 9250) ---------- */

type doqClient struct {
	addr, host string
	mu         sync.Mutex
	conn       *quic.Conn
}

func (c *doqClient) dial(ctx context.Context) (*quic.Conn, time.Duration, error) {
	t0 := time.Now()
	conn, err := quic.DialAddr(ctx, c.addr, tlsConf(c.host, "doq"), &quic.Config{MaxIdleTimeout: 2 * time.Minute, KeepAlivePeriod: 15 * time.Second})
	if err != nil {
		return nil, 0, err
	}
	return conn, time.Since(t0), nil
}

func (c *doqClient) connect(ctx context.Context) (time.Duration, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		c.conn.CloseWithError(0, "")
		c.conn = nil
	}
	conn, d, err := c.dial(ctx)
	if err != nil {
		return 0, err
	}
	c.conn = conn
	return d, nil
}

func (c *doqClient) exchange(ctx context.Context, q []byte) ([]byte, time.Duration, error) {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	for try := 0; ; try++ {
		reused := conn != nil && conn.Context().Err() == nil
		if !reused {
			nc, _, err := c.dial(ctx)
			if err != nil {
				return nil, 0, err
			}
			c.mu.Lock()
			if c.conn != nil {
				c.conn.CloseWithError(0, "")
			}
			c.conn, conn = nc, nc
			c.mu.Unlock()
		}
		m, took, err := doqOnce(ctx, conn, q)
		if err == nil || !reused || try > 0 || ctx.Err() != nil || isTimeout(err) {
			return m, took, err
		}
		conn = nil
	}
}

func doqOnce(ctx context.Context, conn *quic.Conn, q []byte) ([]byte, time.Duration, error) {
	t0 := time.Now()
	s, err := conn.OpenStreamSync(ctx)
	if err != nil {
		return nil, 0, err
	}
	if d, ok := ctx.Deadline(); ok {
		s.SetDeadline(d)
	}
	if err := writeFramed(s, q); err != nil {
		s.CancelRead(0)
		return nil, 0, err
	}
	s.Close() // done sending: the server answers and closes its side
	m, err := readFramed(s)
	if err != nil {
		s.CancelRead(0)
		return nil, 0, err
	}
	return m, time.Since(t0), nil
}

func (c *doqClient) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		c.conn.CloseWithError(0, "")
		c.conn = nil
	}
}
