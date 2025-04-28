# Circle Pinger

A versatile multi-protocol ping utility that supports TCP, UDP, HTTP, and HTTPS protocols. Circle Pinger allows you to test connectivity, measure response times, and diagnose network issues across different protocols.

## Features

- **Multi-Protocol Support**: Ping services using TCP, UDP, HTTP, or HTTPS
- **Detailed Statistics**: Get comprehensive metrics including connection time, DNS resolution time, and more
- **TLS Information**: View TLS certificate details when pinging HTTPS endpoints
- **Custom Timeouts**: Configure connection timeouts and intervals between pings
- **Custom DNS Resolvers**: Specify alternative DNS servers for name resolution
- **HTTP Options**: Set custom HTTP methods, headers, and follow redirects
- **UDP Support**: Test UDP services like DNS servers

## Installation

### From Source

```bash
# Clone the repository
git clone https://github.com/circle-protocol/circle-pinger.git
cd circle-pinger

# Build the binary
go build -o tcping

# Install to your PATH (optional)
sudo mv tcping /usr/local/bin/
```

### Using Go Install

```bash
go install github.com/circle-protocol/circle-pinger@latest
```

## Usage

### Basic Examples

```bash
# TCP ping (default port 80)
tcping google.com

# TCP ping with custom port
tcping google.com 443

# HTTP ping
tcping http://google.com

# HTTPS ping
tcping https://google.com

# UDP ping (e.g., DNS server)
tcping udp://8.8.8.8:53
```

### Command-Line Options

```
Usage:
  tcping host port [flags]

Examples:
  1. ping over tcp
    > tcping google.com
  2. ping over tcp with custom port
    > tcping google.com 443
  3. ping over http
    > tcping http://google.com
  4. ping over https
    > tcping https://google.com
  5. ping over udp (e.g., DNS server)
    > tcping udp://8.8.8.8:53

Flags:
  -c, --counter int           ping counter (default 4)
  -D, --dns-server strings    Use the specified dns resolve server
  -h, --help                  help for tcping
      --http-method string    Use custom HTTP method instead of GET in http mode (default "GET")
  -I, --interval string       ping interval, units are "ns", "us" (or "µs"), "ms", "s", "m", "h" (default "1s")
      --meta                  With meta info
      --proxy string          Use HTTP proxy
  -T, --timeout string        connect timeout, units are "ns", "us" (or "µs"), "ms", "s", "m", "h" (default "1s")
      --user-agent string     Use custom UA in http mode (default "tcping")
  -v, --version               show the version and exit
```

## Examples

### TCP Ping

```bash
# Basic TCP ping to port 80
tcping google.com

# TCP ping to a specific port with 10 pings and 2s timeout
tcping google.com 443 -c 10 -T 2s
```

### HTTP/HTTPS Ping

```bash
# HTTP ping with custom method and user agent
tcping http://api.example.com --http-method POST --user-agent "MyApp/1.0"

# HTTPS ping with meta information (shows TLS details)
tcping https://github.com --meta
```

### UDP Ping

```bash
# UDP ping to DNS server
tcping udp://8.8.8.8:53

# UDP ping with custom timeout and interval
tcping udp://8.8.8.8:53 -T 3s -I 2s
```

### Using Custom DNS Servers

```bash
# Use Cloudflare's DNS server for name resolution
tcping google.com -D 1.1.1.1
```

## Output Format

The output includes:

- Sequence number
- Target address
- Response time
- Status (connected/failed)
- Error message (if any)
- Additional metadata (status code for HTTP, TLS info for HTTPS)

Example output:
```
PING tcp://google.com:80
1: tcp://google.com:80 - connected - time=15.254ms
2: tcp://google.com:80 - connected - time=14.897ms
3: tcp://google.com:80 - connected - time=14.915ms
4: tcp://google.com:80 - connected - time=14.893ms

--- tcp://google.com:80 ping statistics ---
4 probes transmitted, 4 connected, 0% loss
min/avg/max = 14.893/14.990/15.254 ms
```

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

This project is licensed under the MIT License - see the LICENSE file for details.

## Acknowledgments

- Inspired by the traditional `ping` utility
- Thanks to all contributors who have helped shape this project
