
package pinger

import "time"

const (
	DefaultCounter  = 4
	DefaultInterval = time.Second
	DefaultTimeout  = time.Second * 5
)

const (
	// TCP is the TCP protocol.
	TCP Protocol = iota
	// HTTP is the HTTP protocol.
	HTTP
	// HTTPS is the HTTPS protocol.
	HTTPS
	// UDP is the UDP protocol.
	UDP
)