// SPDX-License-Identifier: Apache-2.0

// Command knowledge-collector-loki is a knowledge custom collector for Grafana
// Loki. It serves one MCP tool over stdio; the knowledge daemon spawns it from
// a `type: stdio` entry in the collector config file and calls that tool to
// collect a log graph.
//
// STDOUT IS THE PROTOCOL STREAM. Every diagnostic goes to stderr; a stray print
// to stdout corrupts the JSON-RPC framing and reaches an operator as an opaque
// handshake failure with nothing pointing at its cause.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/fulminate-io/knowledge-contrib/framework"
	"github.com/fulminate-io/knowledge-contrib/loki/internal/collect"
)

func main() {
	// The signal context is what makes a terminated collector stop its walk
	// rather than be killed mid-request: the context reaches every HTTP call.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := framework.ServeStdio(ctx, &collect.Collector{}); err != nil {
		fmt.Fprintf(os.Stderr, "knowledge-collector-loki: %v\n", err)
		os.Exit(1)
	}
}
