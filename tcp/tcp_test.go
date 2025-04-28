package tcp

import (
	"context"
	"testing"

	"github.com/circle-protocol/circle-pinger/pinger"
)

func TestPing(t *testing.T) {
	ping := New("google.com", 80, &pinger.Option{}, false)
	stats := ping.Ping(context.Background())
	if !stats.Connected {
		t.Fatalf("ping failed, %s", stats.Error)
	}
}

func TestPing_Failed(t *testing.T) {
	ping := New("127.0.0.1", 1, &pinger.Option{}, false)
	stats := ping.Ping(context.Background())
	if stats.Connected {
		t.Fatalf("it should be connected refused error")
	}
}
