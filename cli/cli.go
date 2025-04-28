// Package cli provides the command-line interface for the circle-pinger tool.
package cli

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/circle-protocol/circle-pinger/http"
	"github.com/circle-protocol/circle-pinger/pinger"
	"github.com/circle-protocol/circle-pinger/tcp"
	"github.com/circle-protocol/circle-pinger/udp"
	"github.com/circle-protocol/circle-pinger/utils"
	"github.com/spf13/cobra"
)

var (
	// Command-line flags
	showVersion bool
	counter     int
	timeout     string
	interval    string
	sigs        chan os.Signal

	// HTTP-specific flags
	httpMethod string
	httpUA     string

	// DNS server flags
	dnsServer []string
)

// RootCmd is the main command for the circle-pinger CLI
var RootCmd = &cobra.Command{
	Use:   "tcping host port",
	Short: "tcping is a multi-protocol ping tool",
	Long:  "tcping is a ping tool that supports TCP, UDP, HTTP, and HTTPS protocols",
	Example: `
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
	`,
	Run: runCommand,
}

// runCommand is the main function that executes when the CLI is run
func runCommand(cmd *cobra.Command, args []string) {
	// Show version if requested
	if showVersion {
		fmt.Printf("version: %s\n", cmd.Version)
		return
	}

	// Validate arguments
	if len(args) == 0 {
		cmd.Usage()
		return
	}
	if len(args) > 2 {
		cmd.Println("invalid command arguments")
		return
	}

	// Parse the target address
	url, err := utils.ParseAddress(args[0])
	if err != nil {
		fmt.Printf("%s is an invalid target.\n", args[0])
		return
	}

	// Determine port
	defaultPort := "80"
	if port := url.Port(); port != "" {
		defaultPort = port
	} else if url.Scheme == "https" {
		defaultPort = "443"
	} else if url.Scheme == "udp" {
		defaultPort = "53" // Default UDP port (DNS)
	}

	// Override port if provided as second argument
	if len(args) > 1 {
		defaultPort = args[1]
	}

	// Convert port to integer
	port, err := strconv.Atoi(defaultPort)
	if err != nil {
		cmd.Printf("%s is invalid port.\n", defaultPort)
		return
	}
	url.Host = fmt.Sprintf("%s:%d", url.Hostname(), port)

	// Parse timeout and interval durations
	timeoutDuration, err := utils.ParseDuration(timeout)
	if err != nil {
		cmd.Println("parse timeout failed", err)
		cmd.Usage()
		return
	}

	intervalDuration, err := utils.ParseDuration(interval)
	if err != nil {
		cmd.Println("parse interval failed", err)
		cmd.Usage()
		return
	}

	// Determine protocol
	protocol, err := pinger.NewProtocol(url.Scheme)
	if err != nil {
		cmd.Println("invalid protocol", err)
		cmd.Usage()
		return
	}

	// Create pinger options
	option := &pinger.Option{
		Timeout: timeoutDuration,
	}

	// Configure custom DNS resolver if specified
	if len(dnsServer) != 0 {
		option.Resolver = &net.Resolver{
			PreferGo: true,
			Dial: func(ctx context.Context, network, address string) (conn net.Conn, err error) {
				for _, addr := range dnsServer {
					if conn, err = net.Dial("udp", addr+":53"); err != nil {
						continue
					} else {
						return conn, nil
					}
				}
				return
			},
		}
	}

	// Get the appropriate ping factory for the protocol
	pingFactory, ok := pinger.Load(protocol)
	if !ok {
		cmd.Printf("Protocol %s is not supported\n", protocol)
		return
	}

	// Create the ping instance
	p, err := pingFactory(url, option)
	if err != nil {
		cmd.Println("load pinger failed", err)
		cmd.Usage()
		return
	}

	// Create and start the pinger
	pinger := pinger.NewPinger(os.Stdout, url, p, intervalDuration, counter, timeoutDuration)
	sigs = make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go pinger.Ping()

	// Wait for completion or interruption
	select {
	case <-sigs:
	case <-pinger.Done():
	}

	pinger.Stop()
	pinger.Summarize()
}

// fixProxy parses a proxy URL string and sets it in the options
func fixProxy(proxy string, op *pinger.Option) error {
	if proxy == "" {
		return nil
	}
	u, err := url.Parse(proxy)
	op.Proxy = u
	return err
}

// Initialize registers all protocol handlers and sets up command-line flags
func Initialize() {
	// HTTP method and user agent flags
	RootCmd.Flags().StringVar(&httpMethod, "http-method", "GET", `Use custom HTTP method instead of GET in http mode.`)
	ua := RootCmd.Flags().String("user-agent", "tcping", `Use custom UA in http mode.`)

	// Meta info flag
	meta := RootCmd.Flags().Bool("meta", false, `With meta info`)

	// Proxy flag
	proxy := RootCmd.Flags().String("proxy", "", "Use HTTP proxy")

	// Register HTTP protocol handler
	pinger.Register(pinger.HTTP, func(url *url.URL, op *pinger.Option) (pinger.Ping, error) {
		if err := fixProxy(*proxy, op); err != nil {
			return nil, err
		}
		op.UA = *ua
		return http.New(httpMethod, url.String(), op, *meta)
	})

	// Register HTTPS protocol handler
	pinger.Register(pinger.HTTPS, func(url *url.URL, op *pinger.Option) (pinger.Ping, error) {
		if err := fixProxy(*proxy, op); err != nil {
			return nil, err
		}
		op.UA = *ua
		return http.New(httpMethod, url.String(), op, *meta)
	})

	// Register TCP protocol handler
	pinger.Register(pinger.TCP, func(url *url.URL, op *pinger.Option) (pinger.Ping, error) {
		port, err := strconv.Atoi(url.Port())
		if err != nil {
			return nil, err
		}
		return tcp.New(url.Hostname(), port, op, *meta), nil
	})

	// Register UDP protocol handler
	pinger.Register(pinger.UDP, func(url *url.URL, op *pinger.Option) (pinger.Ping, error) {
		port, err := strconv.Atoi(url.Port())
		if err != nil {
			return nil, err
		}
		return udp.New(url.Hostname(), port, op), nil
	})

	// General flags
	RootCmd.Flags().BoolVarP(&showVersion, "version", "v", false, "show the version and exit.")
	RootCmd.Flags().IntVarP(&counter, "counter", "c", pinger.DefaultCounter, "ping counter")
	RootCmd.Flags().StringVarP(&timeout, "timeout", "T", "1s", `connect timeout, units are "ns", "us" (or "µs"), "ms", "s", "m", "h"`)
	RootCmd.Flags().StringVarP(&interval, "interval", "I", "1s", `ping interval, units are "ns", "us" (or "µs"), "ms", "s", "m", "h"`)
	RootCmd.Flags().StringArrayVarP(&dnsServer, "dns-server", "D", nil, `Use the specified dns resolve server.`)
}

// Execute runs the root command
func Execute() error {
	return RootCmd.Execute()
}
