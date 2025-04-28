// Package http provides HTTP ping functionality for the circle-pinger tool.
package http

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http/httptrace"
	"strings"
	"time"
)

// Ensure Trace implements fmt.Stringer
var _ fmt.Stringer = (*Trace)(nil)

// Trace captures detailed timing information about an HTTP request.
type Trace struct {
	DNSDuration time.Duration `json:"dns_duration"`

	connectStart    time.Time
	ConnectDuration time.Duration `json:"connect_duration"`

	tlsStart    time.Time
	tls         bool
	TLSDuration time.Duration `json:"tls_duration"`

	WroteRequestDuration time.Duration `json:"wrote_request_duration"`

	WaitResponseDuration time.Duration `json:"wait_response_duration"`

	BodyDuration time.Duration `json:"body_duration"`

	tlsState tls.ConnectionState

	address string
}

// String returns a formatted string representation of the trace data.
func (t *Trace) String() string {
	// Pre-allocate a reasonable size for the builder
	builder := strings.Builder{}
	builder.Grow(200)

	// Add connect duration
	builder.WriteString("connect=")
	builder.WriteString(t.ConnectDuration.String())

	// Add TLS duration if applicable
	if t.tls {
		builder.WriteString(" tls=")
		builder.WriteString(t.TLSDuration.String())
	}

	// Add request duration
	builder.WriteString(" request=")
	builder.WriteString(t.WroteRequestDuration.String())

	// Add wait response duration
	builder.WriteString(" wait_response=")
	builder.WriteString(t.WaitResponseDuration.String())

	// Add body duration - fixed bug: was using WaitResponseDuration
	builder.WriteString(" response_body=")
	builder.WriteString(t.BodyDuration.String())

	return builder.String()
}

// WithTrace adds HTTP tracing to the provided context.
// It returns a new context with trace hooks installed.
func (t *Trace) WithTrace(ctx context.Context) context.Context {
	start := time.Now()
	var dnsStart, connectStart, tlsStart, writeStart time.Time

	return httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		DNSStart: func(info httptrace.DNSStartInfo) {
			dnsStart = time.Now()
		},
		DNSDone: func(info httptrace.DNSDoneInfo) {
			t.DNSDuration = time.Since(dnsStart)
		},
		ConnectStart: func(network, addr string) {
			connectStart = time.Now()
			t.connectStart = connectStart
			// Extract the address part from addr (host:port)
			host, _, err := net.SplitHostPort(addr)
			if err == nil {
				t.address = host
			} else {
				t.address = addr // Fallback to full addr if parsing fails
			}
		},
		ConnectDone: func(network, addr string, err error) {
			t.ConnectDuration = time.Since(connectStart)
		},
		TLSHandshakeStart: func() {
			tlsStart = time.Now()
			t.tlsStart = tlsStart
			t.tls = true
		},
		TLSHandshakeDone: func(state tls.ConnectionState, err error) {
			t.TLSDuration = time.Since(tlsStart)
			t.tlsState = state
		},
		WroteRequest: func(info httptrace.WroteRequestInfo) {
			// Calculate time spent writing the request, excluding previous phases
			elapsed := time.Since(start)
			t.WroteRequestDuration = elapsed - t.TLSDuration - t.ConnectDuration - t.DNSDuration
			writeStart = time.Now()
		},
		GotFirstResponseByte: func() {
			// Fixed calculation: time between wrote request and first byte
			if !writeStart.IsZero() {
				t.WaitResponseDuration = time.Since(writeStart)
			} else {
				// Fallback if writeStart wasn't set
				elapsed := time.Since(start)
				t.WaitResponseDuration = elapsed - t.TLSDuration - t.ConnectDuration - t.DNSDuration - t.WroteRequestDuration
			}
		},
	})
}
