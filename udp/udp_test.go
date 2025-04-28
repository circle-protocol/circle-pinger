package udp

import (
	"context"
	"testing"
	"time"

	"github.com/circle-protocol/circle-pinger/pinger"
)

func TestPing(t *testing.T) {
	// Test against a known UDP service - DNS on Google's public DNS server (8.8.8.8:53)
	// DNS is a common UDP service that should be widely accessible
	ping := New("8.8.8.8", 53, &pinger.Option{
		Timeout: 2 * time.Second, // Set a reasonable timeout
	})

	stats := ping.Ping(context.Background())

	// For UDP, we might not always get a response even if the service is up
	// because UDP is connectionless. Some UDP services might not respond to
	// random packets. But DNS servers typically do respond to queries.
	if !stats.Connected {
		t.Logf("UDP ping to 8.8.8.8:53 failed: %v", stats.Error)
		t.Logf("This might be due to network restrictions or firewall settings")
		t.Skip("Skipping test as UDP ping to DNS server failed - this may be environment-dependent")
	}

	// If we got here, the ping was successful
	t.Logf("Successfully pinged UDP service at 8.8.8.8:53")
	t.Logf("Round trip time: %v", stats.Duration)
}

func TestPing_Failed(t *testing.T) {
	// Test against a port that's unlikely to have a UDP service
	// and unlikely to respond to our ping packet
	ping := New("127.0.0.1", 54321, &pinger.Option{
		Timeout: 1 * time.Second, // Short timeout for faster test
	})

	stats := ping.Ping(context.Background())

	// For UDP, a failed ping typically means a timeout waiting for a response
	// since there's no explicit connection rejection like in TCP
	if stats.Connected {
		t.Fatalf("Expected ping to fail for non-existent UDP service, but it succeeded")
	}

	// Verify we got an error (likely a timeout)
	if stats.Error == nil {
		t.Fatalf("Expected error for failed UDP ping, but got nil")
	}

	t.Logf("UDP ping correctly failed with error: %v", stats.Error)
}
