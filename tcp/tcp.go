package tcp

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http/httptrace"
	"time"

	"github.com/circle-protocol/circle-pinger/meta"
	"github.com/circle-protocol/circle-pinger/pinger"
)

var _ pinger.Ping = (*Ping)(nil)

func New(host string, port int, op *pinger.Option, tls bool) *Ping {
	return &Ping{
		tls:    tls,
		host:   host,
		port:   port,
		option: op,
		dialer: &net.Dialer{
			Resolver: op.Resolver,
		},
	}
}

type Ping struct {
	option *pinger.Option
	host   string
	port   int
	dialer *net.Dialer
	tls    bool
}

func (p *Ping) Ping(ctx context.Context) *pinger.Stats {
	timeout := pinger.DefaultTimeout
	if p.option.Timeout > 0 {
		timeout = p.option.Timeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var stats pinger.Stats
	var dnsStart time.Time
	// trace dns query
	ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		DNSStart: func(info httptrace.DNSStartInfo) {
			dnsStart = time.Now()
		},
		DNSDone: func(info httptrace.DNSDoneInfo) {
			stats.DNSDuration = time.Since(dnsStart)
		},
	})

	start := time.Now()
	var (
		conn    net.Conn
		err     error
		tlsConn *tls.Conn
		tlsErr  error
	)
	if p.tls {
		tlsConn, err = tls.DialWithDialer(p.dialer, "tcp", fmt.Sprintf("%s:%d", p.host, p.port), &tls.Config{
			InsecureSkipVerify: true,
		})
		if err == nil {
			conn = tlsConn.NetConn()
		} else {
			tlsErr = err
			conn, err = p.dialer.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", p.host, p.port))
		}
	} else {
		conn, err = p.dialer.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", p.host, p.port))
	}
	stats.Duration = time.Since(start)
	if err != nil {
		stats.Error = err
		if oe, ok := err.(*net.OpError); ok && oe.Addr != nil {
			stats.Address = oe.Addr.String()
		}
	} else {
		stats.Connected = true
		stats.Address = conn.RemoteAddr().String()
		if tlsConn != nil && len(tlsConn.ConnectionState().PeerCertificates) > 0 {
			state := tlsConn.ConnectionState()
			stats.Extra = meta.Meta{
				DNSNames:   state.PeerCertificates[0].DNSNames,
				ServerName: state.ServerName,
				Version:    int(state.Version - tls.VersionTLS10),
				NotBefore:  state.PeerCertificates[0].NotBefore,
				NotAfter:   state.PeerCertificates[0].NotAfter,
			}
		} else if p.tls {
			stats.Extra = bytes.NewBufferString(fmt.Sprintf("TLS handshake failed, %s", tlsErr))
		}
	}
	return &stats
}
