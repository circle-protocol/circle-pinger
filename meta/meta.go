package meta

import (
	"fmt"
	"strings"
	"time"
)

var _ fmt.Stringer = (*Meta)(nil)

type Meta struct {
	Version    int
	DNSNames   []string
	ServerName string
	NotBefore  time.Time
	NotAfter   time.Time
}

func (m Meta) String() string {
	return fmt.Sprintf(
		"serverName=%s version=%d notBefore=%s notAfter=%s dnsNames=%s",
		m.ServerName,
		m.Version,
		formatTime(m.NotBefore),
		formatTime(m.NotAfter),
		strings.Join(m.DNSNames, ","),
	)
}

func formatTime(t time.Time) string {
	return t.Format(time.RFC3339)
}
