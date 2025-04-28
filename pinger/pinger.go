package pinger

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"text/template" // Use text/template for non-HTML output
	"time"

	"golang.org/x/sync/errgroup"
)

var (
	// pinger contains registered Factory functions for each Protocol.
	// Access should ideally be guarded if registration can happen concurrently
	// after init, but for typical package init registration, a simple map is fine.
	pinger = make(map[Protocol]Factory)

	// ErrProtocolNotSupported is returned when a requested protocol is not registered.
	ErrProtocolNotSupported = errors.New("protocol not supported")
)

// Factory is a function that creates a Ping instance for a given URL and options.
type Factory func(url *url.URL, op *Option) (Ping, error)

// Register registers a Factory function for a given Protocol.
// It's typically called during package initialization (init functions).
func Register(protocol Protocol, factory Factory) {
	if _, exists := pinger[protocol]; exists {
		// Log or panic if a protocol is registered multiple times?
	}
	pinger[protocol] = factory
}

// Load retrieves the Factory function for a given Protocol.
// Returns the Factory and a boolean indicating if it was found.
func Load(protocol Protocol) (Factory, bool) {
	factory, ok := pinger[protocol]
	return factory, ok
}

// Protocol represents a network protocol for pinging.
type Protocol int

// String returns the string representation of the Protocol.
func (protocol Protocol) String() string {
	// Use a slice or map lookup for string conversion for clarity and potential performance
	// if many protocols existed, but a switch is perfectly fine for a few constants.
	switch protocol {
	case TCP:
		return "tcp"
	case HTTP:
		return "http"
	case HTTPS:
		return "https"
	case UDP:
		return "udp"
	default:
		// Return a specific string for unknown protocols
		return "unknown"
	}
}

// NewProtocol converts a protocol string to a Protocol enum
// It is case-insensitive.
func NewProtocol(protocolStr string) (Protocol, error) {
	switch strings.ToLower(protocolStr) {
	case TCP.String():
		return TCP, nil
	case HTTP.String():
		return HTTP, nil
	case HTTPS.String():
		return HTTPS, nil
	case UDP.String():
		return UDP, nil
	default:
		// Use the defined error constant
		return 0, fmt.Errorf("%w: %s", ErrProtocolNotSupported, protocolStr)
	}
}

// Option contains configuration options for creating a Ping instance.
type Option struct {
	Timeout time.Duration // Timeout for the entire ping sequence or related operations
	// Resolver is used to customize DNS resolution. Ping implementations might use this.
	Resolver *net.Resolver
	// Proxy is used to configure proxy settings. Ping implementations might use this.
	Proxy *url.URL
	// UA is the User-Agent string for HTTP/S pings. Ping implementations might use this.
	UA string

	// Add other relevant options here as needed
}

// Target represents the destination for a ping operation.
// Note: The Proxy field is a string here. If the Ping implementation
// uses this for connection setup, converting it to *url.URL would be more robust
// and consistent with Option.Proxy. Keeping it as string for now to match original.
type Target struct {
	Protocol Protocol
	Host     string
	IP       string // Resolved IP address, might be set by the Ping implementation
	Port     int
	Proxy    string // Proxy address string, seems redundant with Option.Proxy?

	// Note: Counter and Interval are Pinger-level configurations, not Target-level.
	// Moving them out of Target is cleaner if they only apply to the Pinger's run loop.
	// Let's keep them here for now to match the original structure but note this.
	Counter  int           // Number of ping attempts
	Interval time.Duration // Interval between attempts
	Timeout  time.Duration // Timeout for *each* ping attempt (overrides Option.Timeout?)
	// Clarification needed: Is Target.Timeout for *each* ping or the total?
	// Assuming Target.Timeout is for *each* ping attempt, overriding Option.Timeout.
	// If 0, use Option.Timeout or DefaultTimeout.
}

// String returns a formatted string representation of the Target.
func (target Target) String() string {
	// Use %s for protocol string conversion
	return fmt.Sprintf("%s://%s:%d", target.Protocol, target.Host, target.Port)
}

// StringerFunc is a function type that implements fmt.Stringer
type StringerFunc func() string

// String implements the fmt.Stringer interface for StringerFunc
func (f StringerFunc) String() string {
	return f()
}

// Stats holds the results of a single ping attempt.
type Stats struct {
	Connected   bool                    `json:"connected"`   // True if connection was successful
	Error       error                   `json:"error"`       // Error, if any
	Duration    time.Duration           `json:"duration"`    // Round trip time
	DNSDuration time.Duration           `json:"DNSDuration"` // DNS lookup time, if applicable
	Address     string                  `json:"address"`     // The actual address connected to (IP:Port)
	Meta        map[string]fmt.Stringer `json:"meta"`        // Extra metadata
	Extra       fmt.Stringer            `json:"extra"`       // Additional output, typically multi-line
}

// FormatMeta formats the metadata map into a space-separated key=value string.
// It sorts keys alphabetically for consistent output.
func (s *Stats) FormatMeta() string {
	if len(s.Meta) == 0 {
		return ""
	}

	// Use a slice for keys
	keys := make([]string, 0, len(s.Meta))
	for key := range s.Meta {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	// Estimate total length for the builder to reduce reallocations
	estimatedLen := 0
	for _, key := range keys {
		estimatedLen += len(key) + 1 // key + "="
		if val, ok := s.Meta[key]; ok && val != nil {
			estimatedLen += len(val.String())
		}
		estimatedLen += 1 // space separator
	}
	if estimatedLen > 0 {
		estimatedLen-- // Remove last space estimate
	}

	var builder strings.Builder
	builder.Grow(estimatedLen) // Pre-allocate builder capacity

	for i, key := range keys {
		if i > 0 {
			builder.WriteByte(' ') // WriteByte is efficient
		}
		builder.WriteString(key)
		builder.WriteByte('=')
		// Safely get value and handle nil Stringer
		if val, ok := s.Meta[key]; ok && val != nil {
			builder.WriteString(val.String())
		} else {
			builder.WriteString("<nil>") // Or some other indicator for nil value
		}
	}

	return builder.String()
}

// Ping defines the interface for a pingable target.
type Ping interface {
	// Ping attempts to connect to the target and returns Stats.
	// It takes a context for cancellation and timeout.
	Ping(ctx context.Context) *Stats
}

// Pinger manages the pinging process for a single target.
type Pinger struct {
	ping Ping     // The specific Ping implementation (TCP, HTTP, etc.)
	url  *url.URL // The target URL

	stopOnce sync.Once     // Ensures the stop channel is closed only once
	stopC    chan struct{} // Channel to signal stopping the pinger

	out io.Writer // Where to write output (e.g., os.Stdout)

	interval time.Duration // Time between pings
	counter  int           // Number of pings to send (0 means infinite)
	timeout  time.Duration // Timeout for each individual ping attempt

	// Stats tracking
	minDuration   time.Duration // Minimum duration seen
	maxDuration   time.Duration // Maximum duration seen
	totalDuration time.Duration // Sum of all successful durations
	total         int           // Total number of pings sent
	failedTotal   int           // Total number of failed pings

	// Mutex for protecting stats updates if logStats could be called concurrently
	// (not the case in the current Ping loop, but good practice if it could be)
	// statsMu sync.Mutex
}

// NewPinger creates a new Pinger instance.
// It requires the Ping implementation, target URL, output writer, interval, counter, and timeout.
func NewPinger(out io.Writer, url *url.URL, ping Ping, interval time.Duration, counter int, timeout time.Duration) *Pinger {
	// Apply defaults if necessary
	if interval <= 0 {
		interval = DefaultInterval
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	return &Pinger{
		ping:     ping,
		url:      url,
		stopC:    make(chan struct{}),
		out:      out,
		interval: interval,
		counter:  counter,
		timeout:  timeout, // Store the individual ping timeout
		// minDuration is initialized to a large value in Ping() before the loop
	}
}

// Stop signals the Pinger to stop after the current ping attempt finishes.
func (p *Pinger) Stop() {
	p.stopOnce.Do(func() {
		close(p.stopC)
	})
}

// Done returns a channel that is closed when the Pinger has stopped.
func (p *Pinger) Done() <-chan struct{} {
	return p.stopC
}

// Ping starts the pinging process. It runs until the counter is reached,
// an error occurs, or Stop() is called.
func (p *Pinger) Ping() {
	// Use errgroup.WithContext for structured concurrency and cancellation propagation
	// The context returned by WithContext is cancelled if any goroutine returns a non-nil error.
	group, ctx := errgroup.WithContext(context.Background())

	// Goroutine to listen for the stop signal and cancel the context
	group.Go(func() error {
		select {
		case <-p.Done():
			// Signal received, return context.Canceled to stop the errgroup
			return context.Canceled
		case <-ctx.Done():
			// Context was cancelled from elsewhere (e.g., another goroutine error)
			return ctx.Err()
		}
	})

	// Initialize minDuration before the loop starts
	p.minDuration = time.Duration(math.MaxInt64)

	// Start the main ping loop goroutine
	group.Go(func() error {
		// Trigger the first ping immediately or after a short initial delay
		// The original code used NewTimer(1), which gives an immediate/very short delay.
		// Let's match that by creating a timer that fires immediately.
		timer := time.NewTimer(time.Duration(0)) // Fire immediately
		defer timer.Stop()

		for {
			select {
			case <-timer.C:
				// Time to send a ping

				// Create a context with the configured timeout for this specific ping
				pingCtx, pingCancel := context.WithTimeout(ctx, p.timeout)
				stats := p.ping.Ping(pingCtx) // Perform the ping
				pingCancel()                  // Release resources associated with the timeout context

				// Log and update statistics for the completed ping
				p.logStats(stats)

				// Check if we've reached the desired number of pings
				p.total++
				if p.counter > 0 && p.total >= p.counter {
					// Reached counter limit, stop the pinger gracefully
					p.Stop()   // Signal stop to the other goroutine
					return nil // Exit this goroutine
				}

				// Reset the timer for the next interval, but only if the loop continues
				select {
				case <-ctx.Done():
					// Context cancelled while waiting to reset timer, exit
					return ctx.Err()
				default:
					// Context is still active, reset timer for the next ping
					timer.Reset(p.interval)
				}

			case <-ctx.Done():
				// Context was cancelled (either by Stop() or another goroutine's error)
				// Exit the loop
				return ctx.Err()
			}
		}
	})

	// Wait for all goroutines in the group to finish.
	// g.Wait() returns the error from the first goroutine that failed,
	// or context.Canceled if the context was cancelled.
	if err := group.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		// Log the error if it's not just a cancellation
		p.logError(err)
	}

	// Defer p.Stop() is handled by the function defer, ensuring it's called
	// even if Ping() exits early due to an error or return.
}

// logError writes a formatted error message to the output writer.
func (p *Pinger) logError(err error) {
	// Check if the output writer is configured
	if p.out != nil {
		fmt.Fprintf(p.out, "Pinger runtime error: %v\n", err) // Use a more descriptive message
	}
}

// Summarize prints the ping statistics summary to the output writer.
func (p *Pinger) Summarize() {
	// Use a text template for formatting the summary
	const summaryTpl = `
Ping statistics {{.URL}}
    {{.Total}} probes sent.
    {{.SuccessTotal}} successful, {{.FailedTotal}} failed.
Approximate trip times:{{if .Total}}
    Minimum = {{.MinDuration}}, Maximum = {{.MaxDuration}}, Average = {{.AvgDuration}}{{else}}
    No probes completed successfully.{{end}}` // Add conditional for no probes

	t := template.Must(template.New("summary").Parse(summaryTpl))

	// Create a data structure for template execution, including calculated values
	summaryData := struct {
		URL          *url.URL
		Total        int
		SuccessTotal int
		FailedTotal  int
		MinDuration  time.Duration
		MaxDuration  time.Duration
		AvgDuration  time.Duration
	}{
		URL:          p.url,
		Total:        p.total,
		SuccessTotal: p.total - p.failedTotal,
		FailedTotal:  p.failedTotal,
		MinDuration:  p.minDuration,
		MaxDuration:  p.maxDuration,
		AvgDuration:  0, // Initialize to 0, calculate below
	}

	// Calculate average only if total is greater than 0 to avoid division by zero
	if p.total > 0 {
		summaryData.AvgDuration = p.totalDuration / time.Duration(p.total)
	} else {
		// Set min/max to 0 or a placeholder if no pings completed
		summaryData.MinDuration = 0
		summaryData.MaxDuration = 0
	}

	// Use a bytes.Buffer to capture the template output before writing
	var buf bytes.Buffer
	// Execute the template, writing to the buffer
	if err := t.Execute(&buf, summaryData); err != nil {
		// Handle template execution error - perhaps log it or write an error message
		fmt.Fprintf(p.out, "Error formatting summary: %v\n", err)
		return // Stop if template execution failed
	}

	// Write the buffer content to the output writer
	if p.out != nil {
		_, err := buf.WriteTo(p.out)
		if err != nil {
			// Handle write error - log or ignore depending on context
			// For typical stdout, ignoring is often acceptable, but let's log
			// for robustness in case out is something else.
			fmt.Fprintf(os.Stderr, "Error writing summary output: %v\n", err)
		}
	}
}

// formatError provides a user-friendly string representation of an error.
func (p *Pinger) formatError(err error) string {
	if err == nil {
		return "" // No error
	}

	// Use errors.Is for checking specific error types/values
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return "timeout"
	}

	// Use errors.As for unwrapping specific error types
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		// Recurse into the underlying error if it's a URL error
		return p.formatError(urlErr.Err)
	}

	var netErr net.Error
	if errors.As(err, &netErr) {
		// Check for timeout specifically again, though handled by DeadlineExceeded above
		if netErr.Timeout() {
			return "timeout"
		}
		// Unpack OpError to get potentially more specific system call errors
		var opErr *net.OpError
		if errors.As(netErr, &opErr) {
			// Just use the underlying error directly instead of trying to unwrap it with errors.As
			if opErr.Err != nil {
				return opErr.Err.Error()
			}
		}
		// If it's a net.Error but not timeout or specific OpError type, return its string
		return netErr.Error()
	}

	// For any other error type, return the default error string
	return err.Error()
}

// logStats logs the results of a single ping attempt and updates the statistics.
func (p *Pinger) logStats(stats *Stats) {
	// Use mutex if stats updates could be concurrent (not currently needed but good practice)
	// p.statsMu.Lock()
	// defer p.statsMu.Unlock()

	// Update statistics only if the ping was successful in connecting,
	// but count failed attempts regardless.
	if stats.Connected {
		// Only update duration stats for successful connections
		if stats.Duration < p.minDuration {
			p.minDuration = stats.Duration
		}
		if stats.Duration > p.maxDuration {
			p.maxDuration = stats.Duration
		}
		p.totalDuration += stats.Duration
	}

	// Count failures, but ignore context cancellation errors as explicit failures
	if stats.Error != nil && !errors.Is(stats.Error, context.Canceled) {
		p.failedTotal++
	}

	// Format the main output line using a single fmt.Fprintf
	status := "Failed"
	errorDetail := ""
	if stats.Connected {
		status = "connected"
	}
	if stats.Error != nil {
		errorDetail = fmt.Sprintf("(%s)", p.formatError(stats.Error))
	}

	// Build the basic format string dynamically based on error presence
	// Example: "Ping %s(%s) %s%s - time=%s dns=%s"
	// URL, Address, Status, ErrorDetail, Duration, DNSDuration

	// Check for nil values before calling String() or accessing fields
	urlStr := "<nil>"
	if p.url != nil {
		urlStr = p.url.String()
	}
	addrStr := "<unknown>"
	if stats != nil { // Ensure stats is not nil
		addrStr = stats.Address
	}
	durationStr := "<N/A>"
	if stats != nil {
		durationStr = stats.Duration.String()
	}
	dnsDurationStr := "<N/A>"
	if stats != nil {
		dnsDurationStr = stats.DNSDuration.String()
	}

	// Using Fprintf directly for efficiency and control over output writer
	if p.out != nil {
		_, _ = fmt.Fprintf(p.out, "Ping %s(%s) %s%s - time=%s dns=%s",
			urlStr,
			addrStr,
			status,
			errorDetail,
			durationStr,
			dnsDurationStr,
		)

		// Append metadata if present
		if stats != nil && len(stats.Meta) > 0 {
			_, _ = fmt.Fprintf(p.out, " %s", stats.FormatMeta())
		}

		// Append a newline
		_, _ = fmt.Fprint(p.out, "\n")

		// Append extra info if present
		if stats != nil && stats.Extra != nil {
			extraStr := strings.TrimSpace(stats.Extra.String())
			if extraStr != "" {
				_, _ = fmt.Fprintf(p.out, " %s\n", extraStr)
			}
		}
	}
}

// Result holds the final aggregated statistics for a ping sequence.
// This seems somewhat redundant with the Pinger's internal stats fields.
// It might be used for returning results from a function that *runs* a Pinger
// and aggregates its stats, but it duplicates the Pinger's stats.
// Keeping it for now to match the original, but consider if it's truly necessary
// or if Pinger itself should just expose methods to get final stats.
type Result struct {
	Counter        int     // Total probes attempted (should match Pinger.total?)
	SuccessCounter int     // Successful probes (should match Pinger.total - Pinger.failedTotal?)
	Target         *Target // The target of the ping sequence
	MinDuration    time.Duration
	MaxDuration    time.Duration
	TotalDuration  time.Duration // Sum of successful durations (should match Pinger.totalDuration?)
}

// Avg returns the average duration of successful pings.
// Handles the case of zero successful pings.
func (result Result) Avg() time.Duration {
	if result.SuccessCounter == 0 {
		return 0 // Avoid division by zero
	}
	return result.TotalDuration / time.Duration(result.SuccessCounter)
}

// Failed returns the number of failed pings.
func (result Result) Failed() int {
	return result.Counter - result.SuccessCounter
}

// String returns a formatted summary string for the Result.
func (result Result) String() string {
	// Use a text template for formatting the summary
	const resultTpl = `
Ping statistics {{.Target}}
    {{.Counter}} probes sent.
    {{.SuccessCounter}} successful, {{.Failed}} failed.
Approximate trip times:{{if .SuccessCounter}}
    Minimum = {{.MinDuration}}, Maximum = {{.MaxDuration}}, Average = {{.Avg}}{{else}}
    No successful probes.{{end}}` // Add conditional for no successful pings

	t := template.Must(template.New("result").Parse(resultTpl))

	// Use a bytes.Buffer to capture the template output
	var res bytes.Buffer
	// Execute the template, writing to the buffer
	if err := t.Execute(&res, result); err != nil {
		// Handle template execution error - log and return a basic string
		fmt.Fprintf(os.Stderr, "Error executing result template: %v\n", err)
		return fmt.Sprintf("Ping statistics %v (Error formatting results)", result.Target)
	}
	return res.String()
}
