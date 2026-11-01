package main

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"net/netip"
	"strings"

	"golang.org/x/net/dns/dnsmessage"
)

// reply is what one DNS answer says, as far as the test needs it.
type reply struct {
	RCode dnsmessage.RCode
	AD    bool     // the resolver vouches the answer passed DNSSEC validation
	Addrs []string // A and AAAA records
	TXT   []string
}

var (
	errID       = errors.New("reply for another query")
	errQuestion = errors.New("reply for another name")
)

func randID() uint16 {
	var b [2]byte
	rand.Read(b[:])
	return binary.BigEndian.Uint16(b[:])
}

// randLabel is a name no resolver can have cached: 12 random letters and digits.
func randLabel() string {
	const abc = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 12)
	rand.Read(b)
	for i := range b {
		b[i] = abc[int(b[i])%len(abc)]
	}
	return string(b)
}

func fqdn(name string) string {
	if strings.HasSuffix(name, ".") {
		return name
	}
	return name + "."
}

// newQuery builds a recursive query with EDNS (1232-byte UDP size, as DNS Flag Day 2020 recommends). The AD bit asks
// a validating resolver to say whether the answer is DNSSEC-validated (RFC 6840), without asking for signatures.
func newQuery(name string, t dnsmessage.Type, id uint16) ([]byte, error) {
	n, err := dnsmessage.NewName(fqdn(name))
	if err != nil {
		return nil, err
	}
	b := dnsmessage.NewBuilder(make([]byte, 0, 128), dnsmessage.Header{ID: id, RecursionDesired: true, AuthenticData: true})
	b.EnableCompression()
	if err := b.StartQuestions(); err != nil {
		return nil, err
	}
	if err := b.Question(dnsmessage.Question{Name: n, Type: t, Class: dnsmessage.ClassINET}); err != nil {
		return nil, err
	}
	if err := b.StartAdditionals(); err != nil {
		return nil, err
	}
	var rh dnsmessage.ResourceHeader
	if err := rh.SetEDNS0(1232, dnsmessage.RCodeSuccess, false); err != nil {
		return nil, err
	}
	if err := b.OPTResource(rh, dnsmessage.OPTResource{}); err != nil {
		return nil, err
	}
	return b.Finish()
}

func msgID(m []byte) uint16 {
	if len(m) < 2 {
		return 0
	}
	return binary.BigEndian.Uint16(m)
}

// parseReply checks that msg answers query q (same ID, same question) and reads it.
func parseReply(q, msg []byte) (reply, error) {
	var p dnsmessage.Parser
	h, err := p.Start(msg)
	if err != nil {
		return reply{}, err
	}
	if !h.Response || h.ID != msgID(q) {
		return reply{}, errID
	}
	var qp dnsmessage.Parser
	if _, err := qp.Start(q); err != nil {
		return reply{}, err
	}
	want, err := qp.Question()
	if err != nil {
		return reply{}, err
	}
	got, err := p.Question()
	if err != nil {
		return reply{}, err
	}
	if !strings.EqualFold(got.Name.String(), want.Name.String()) || got.Type != want.Type {
		return reply{}, errQuestion
	}
	if err := p.SkipAllQuestions(); err != nil {
		return reply{}, err
	}
	r := reply{RCode: h.RCode, AD: h.AuthenticData}
	for {
		rh, err := p.AnswerHeader()
		if err == dnsmessage.ErrSectionDone {
			break
		}
		if err != nil {
			return r, nil // a malformed record after the header: what was read is enough
		}
		switch rh.Type {
		case dnsmessage.TypeA:
			a, err := p.AResource()
			if err != nil {
				return r, nil
			}
			r.Addrs = append(r.Addrs, netip.AddrFrom4(a.A).String())
		case dnsmessage.TypeAAAA:
			a, err := p.AAAAResource()
			if err != nil {
				return r, nil
			}
			r.Addrs = append(r.Addrs, netip.AddrFrom16(a.AAAA).String())
		case dnsmessage.TypeTXT:
			t, err := p.TXTResource()
			if err != nil {
				return r, nil
			}
			r.TXT = append(r.TXT, strings.Join(t.TXT, ""))
		default:
			if err := p.SkipAnswer(); err != nil {
				return r, nil
			}
		}
	}
	return r, nil
}
