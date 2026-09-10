// SPDX-License-Identifier: Apache-2.0

// Command gcp is a knowledge collector for Google Cloud. It serves one MCP tool
// over stdio; a daemon spawns it, calls that tool with a collect id and a
// project, and writes what it returns into a graph of its own.
//
// IT AUTHENTICATES ONLY THROUGH APPLICATION DEFAULT CREDENTIALS and it takes no
// credential from its caller. Every name that chain reads must be present in the
// environment block of this collector's own config entry, because a stdio child
// receives exactly what its entry declares and nothing else.
//
// STDOUT IS THE PROTOCOL STREAM. Nothing in this program writes to it: every
// diagnostic goes to stderr through slog, and a stray print anywhere in the
// walk would corrupt the framing and reach an operator as an opaque handshake
// failure with no message worth reading.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/fulminate-io/knowledge-contrib/framework"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpclients"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/walk"
)

func main() {
	// A terminating signal cancels the walk rather than killing it mid-flight,
	// so the clients it opened are released and a partial enumeration is
	// reported as incomplete rather than vanishing.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	collector := walk.Collector{Enumerations: gcpclients.Enumerations}
	if err := framework.ServeStdio(ctx, collector); err != nil {
		// STDERR, and a non-zero exit. The spawning daemon reads both, and a
		// collector that failed silently with status zero would be recorded as a
		// successful collect that found nothing.
		fmt.Fprintf(os.Stderr, "gcp collector: %v\n", err)
		os.Exit(1)
	}
}
