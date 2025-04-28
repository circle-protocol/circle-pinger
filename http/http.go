// Package http provides HTTP ping functionality for the circle-pinger tool.
package http

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	pkgurl "net/url"
	"strconv"
	"time"

	"github.com/circle-protocol/circle-pinger/pinger"
)

// Ensure Ping implements the pinger.Ping interface
var _ pinger.Ping = (*Ping)(nil)

// New creates a new HTTP Ping instance.
// It validates the method and URL, then configures an HTTP client with appropriate settings.
// If method is empty, it defaults to GET.
func New(method string, url string, op *pinger.Option, trace bool) (*Ping, error) {
	// Handle nil option gracefully
	if op == nil {
		op = &pinger.Option{}
	}

	// Set default method if empty
	if method == "" {
		method = http.MethodGet
	}

	// Validate the URL and method by attempting to create a request
	_, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, fmt.Errorf("url or method is invalid: %w", err)
	}

	// Create transport with appropriate settings
	transport := &http.Transport{
		Proxy: func(r *http.Request) (*pkgurl.URL, error) {
			if op.Proxy != nil {
				return op.Proxy, nil
			}
			return http.ProxyFromEnvironment(r)
		},
		DialContext: (&net.Dialer{
			Resolver: op.Resolver,
			Timeout:  30 * time.Second, // Reasonable default dial timeout
		}).DialContext,
		DisableKeepAlives:     true,  // Don't reuse connections
		ForceAttemptHTTP2:     false, // Stick to HTTP/1.1 for simplicity
		MaxIdleConnsPerHost:   -1,    // Disable idle connections since we're not reusing them
		IdleConnTimeout:       0,     // No idle connections
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	// Create client with appropriate settings
	client := &http.Client{
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Disable redirects - we want to measure just the initial request
			return http.ErrUseLastResponse
		},
		Timeout: 0, // We'll handle timeout with context
	}

	return &Ping{
		url:    url,
		method: method,
		trace:  trace,
		option: op,
		client: client,
	}, nil
}

// Ping represents an HTTP ping operation.
type Ping struct {
	client *http.Client
	trace  bool
	option *pinger.Option
	method string
	url    string
}

// Ping performs an HTTP request and collects timing statistics.
// It uses the configured HTTP client to make a request to the target URL,
// and measures various aspects of the HTTP transaction.
func (p *Ping) Ping(ctx context.Context) *pinger.Stats {
	// Determine timeout
	timeout := pinger.DefaultTimeout
	if p.option != nil && p.option.Timeout > 0 {
		timeout = p.option.Timeout
	}

	// Create timeout context
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Initialize stats
	stats := &pinger.Stats{
		Meta: make(map[string]fmt.Stringer),
	}

	// Initialize trace if enabled
	trace := Trace{}
	if p.trace {
		stats.Extra = &trace
		ctx = trace.WithTrace(ctx)
	}

	// Start timing
	start := time.Now()

	// Create request
	req, err := http.NewRequestWithContext(ctx, p.method, p.url, nil)
	if err != nil {
		stats.Error = err
		stats.Duration = time.Since(start)
		return stats
	}

	// Set user agent if provided
	if p.option != nil && p.option.UA != "" {
		req.Header.Set("User-Agent", p.option.UA)
	}

	// Execute request
	resp, err := p.client.Do(req)

	// Capture DNS and address info from trace
	stats.DNSDuration = trace.DNSDuration
	stats.Address = trace.address

	// Handle request error
	if err != nil {
		stats.Error = err
		stats.Duration = time.Since(start)
		return stats
	}

	// Request succeeded
	defer resp.Body.Close()
	stats.Connected = true
	stats.Meta["status"] = Int(resp.StatusCode)

	// Measure body read time
	bodyStart := time.Now()
	n, err := io.Copy(io.Discard, resp.Body)
	bodyReadTime := time.Since(bodyStart)
	trace.BodyDuration = bodyReadTime

	// Record bytes read if any
	if n > 0 {
		stats.Meta["bytes"] = Int(n)
	}

	// Calculate total duration
	stats.Duration = time.Since(start)

	// Handle body read error
	if err != nil {
		stats.Connected = false
		stats.Error = fmt.Errorf("read body failed: %w", err)
	}

	return stats
}

// Int is a simple wrapper around int that implements fmt.Stringer.
type Int int

// String returns the string representation of the Int.
func (i Int) String() string {
	return strconv.Itoa(int(i))
}
