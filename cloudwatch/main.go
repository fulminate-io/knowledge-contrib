// SPDX-License-Identifier: Apache-2.0

// Command cloudwatch serves the AWS CloudWatch Logs knowledge collector over MCP
// on stdin and stdout.
//
// It is spawned by the knowledge daemon from a collector configuration entry
// and speaks the collector contract; it is not run interactively.
//
// STDOUT IS THE PROTOCOL STREAM, so this program writes nothing to it. Its one
// diagnostic — a serving failure — goes to stderr and the process exits
// non-zero, which is what the spawning daemon reports.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

func main() {
	// The session ends when the parent closes the pipes, but an interrupt or a
	// termination signal has to reach the walk too: a collect in flight holds
	// an open AWS request, and canceling the context is what stops it paging.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := framework.ServeStdio(ctx, New()); err != nil {
		fmt.Fprintf(os.Stderr, "cloudwatch: %v\n", err)
		os.Exit(1)
	}
}
