package main

import (
	"fmt"
	"os"

	"github.com/circle-protocol/circle-pinger/cli"
)

// Version information (set during build)
var (
	version   = "dev"
	gitCommit = "unknown"
)

func main() {
	// Set version information
	cli.RootCmd.Version = version

	// Initialize the CLI
	cli.Initialize()

	// Execute the CLI
	if err := cli.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
