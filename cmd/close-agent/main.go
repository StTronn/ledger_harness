// Command close-agent is the terminal-first entrypoint for the close engine.
// It is intentionally thin: all command wiring lives in internal/cli so it can
// be unit-tested. main only bridges os.Args/stdout and the process exit code.
package main

import (
	"os"

	"github.com/razorpay/close-agent/internal/cli"
)

func main() {
	if err := cli.Execute(os.Args[1:], os.Stdout); err != nil {
		os.Exit(1)
	}
}
