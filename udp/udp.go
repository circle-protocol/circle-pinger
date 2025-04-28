package udp // This implementation is in its own package

import (
	"context"
	"fmt"
	"net"
	"strconv" // Needed to convert port int to string
	"time"

	"github.com/circle-protocol/circle-pinger/pinger"
)

// Ensure that our Ping struct implements the pinger.Ping interface
var _ pinger.Ping = (*Ping)(nil)

// New creates a new UDP Ping instance.
// It takes host and port as arguments, along with optional configuration.
func New(host string, port int, op *pinger.Option) *Ping {
	// Handle nil option gracefully
	if op == nil {
		op = &pinger.Option{}
	}

	return &Ping{
		host:   host,
		port:   port,
		option: op,
		dialer: &net.Dialer{
			Resolver: op.Resolver, // Use resolver from option
		},
	}
}

// Ping is the UDP ping implementation.
// It sends a UDP packet to the target and waits for a response or timeout.
func (p *Ping) Ping(ctx context.Context) *pinger.Stats {
	// Determine the timeout for this specific ping attempt.
	// Use option timeout if positive, otherwise use the package default.
	timeout := pinger.DefaultTimeout
	if p.option != nil && p.option.Timeout > 0 {
		timeout = p.option.Timeout
	}

	// Create a context with the calculated timeout for this ping attempt.
	// This context will be used for DNS lookup, dialing, writing, and reading.
	pingCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel() // Ensure cancel is called to release resources

	stats := &pinger.Stats{
		Connected: false,                         // Assume not connected until successful read
		Meta:      make(map[string]fmt.Stringer), // Initialize meta map
	}

	// Measure total time for the entire ping attempt
	startTotal := time.Now()

	// --- Address Resolution (Manual for separate DNS timing) ---
	var resolvedIP string
	var dnsErr error
	startDNS := time.Now()

	// Attempt to parse the host as an IP first
	if ip := net.ParseIP(p.host); ip != nil {
		// It's already an IP address, no DNS lookup needed
		resolvedIP = ip.String()
		stats.DNSDuration = 0 // No DNS time
	} else {
		// It's a hostname, perform DNS lookup using the dialer's resolver or default
		// Use LookupIPContext for context-aware DNS resolution
		resolver := net.DefaultResolver // Use default resolver or p.dialer.Resolver if preferred
		if p.dialer != nil && p.dialer.Resolver != nil {
			resolver = p.dialer.Resolver
		}

		ips, lookupErr := resolver.LookupIP(pingCtx, "ip", p.host) // "ip" network type for both IPv4 and IPv6
		stats.DNSDuration = time.Since(startDNS)                   // Record DNS duration

		if lookupErr != nil {
			dnsErr = fmt.Errorf("dns lookup failed: %w", lookupErr)
			stats.Error = dnsErr // Record the DNS error
			// If DNS fails, the ping attempt fails here.
			stats.Duration = time.Since(startTotal) // Total time includes failed DNS
			return stats
		}
		if len(ips) == 0 {
			// Should not happen if LookupIP didn't return an error, but defensive check
			stats.Error = fmt.Errorf("dns lookup returned no IP addresses for %s", p.host)
			stats.Duration = time.Since(startTotal)
			return stats
		}
		// Use the first resolved IP address (usually sufficient for ping)
		resolvedIP = ips[0].String()
	}

	// Construct the target address using the resolved IP and port
	targetAddr := net.JoinHostPort(resolvedIP, strconv.Itoa(p.port))
	stats.Address = targetAddr // Record the address used

	// --- UDP Connection and Ping Attempt ---

	// Use the dialer with DialContext for timeout-aware dialing.
	// For UDP, DialContext doesn't truly establish a connection,
	// but it binds the local socket and associates it with the remote address.
	// The Dialer timeout applies to the Dial call itself (e.g., initial setup, immediate errors).
	conn, dialErr := p.dialer.DialContext(pingCtx, "udp", targetAddr)
	if dialErr != nil {
		stats.Error = fmt.Errorf("dial failed: %w", dialErr)
		stats.Duration = time.Since(startTotal) // Total time includes failed dial
		// If there was a DNS error before, dialErr will overwrite it. This seems acceptable.
		return stats
	}
	defer conn.Close() // Ensure the UDP connection is closed

	// Set a read deadline on the connection using the remaining time from the context.
	// This is crucial for the Read() call to time out if no response is received.
	if deadline, ok := pingCtx.Deadline(); ok {
		conn.SetReadDeadline(deadline)
	} else {
		// Fallback, should not be hit with context.WithTimeout above
		conn.SetReadDeadline(time.Now().Add(timeout))
	}

	// Send a small UDP packet. The content isn't critical for basic reachability.
	// A small payload like a single byte or a timestamp is common.
	sendData := []byte("ping") // Simple payload
	_, writeErr := conn.Write(sendData)
	if writeErr != nil {
		stats.Error = fmt.Errorf("write failed: %w", writeErr)
		stats.Duration = time.Since(startTotal) // Total time includes write failure
		return stats
	}

	// Attempt to read a response from the connection.
	// This call will block until:
	// 1. A UDP packet is received from the remote address.
	// 2. The read deadline is reached (timeout).
	// 3. An ICMP error (like Port Unreachable) is received by the OS
	//    and potentially surfaced by the Read call as a socket error.
	readBuf := make([]byte, 1024)    // Buffer to read into
	_, readErr := conn.Read(readBuf) // Read from the connection

	// Stop the total timer right after the read attempt finishes
	stats.Duration = time.Since(startTotal)

	// Check the result of the read operation
	if readErr == nil {
		// Success! Received a UDP response packet.
		stats.Connected = true
		stats.Error = nil // Clear any prior DNS error if successful response indicates host is fine
		// stats.Duration already contains the Round Trip Time (send + wait + receive)
	} else {
		// Read failed (timeout, ICMP error surfaced as socket error, etc.)
		stats.Connected = false
		// Read errors might include context.DeadlineExceeded or network errors
		stats.Error = fmt.Errorf("read failed: %w", readErr)
		// The pinger's logStats function will use formatError to make this user-friendly.
	}

	// Add sent/received byte count to meta if desired
	stats.Meta["sent"] = pinger.StringerFunc(func() string { return strconv.Itoa(len(sendData)) })
	// Note: Received byte count is tricky if readBuf wasn't fully filled or if errors occurred.
	// For simplicity, we can omit the received count or only include on success.
	// if readErr == nil {
	//    stats.Meta["recv"] = pinger.StringerFunc(func() string { return strconv.Itoa(n) }) // 'n' from conn.Read(readBuf[:n])
	// }

	return stats
}

// Ping struct definition
type Ping struct {
	option *pinger.Option
	host   string
	port   int
	dialer *net.Dialer // Dialer to potentially use custom resolver
}
