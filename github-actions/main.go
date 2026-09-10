// SPDX-License-Identifier: Apache-2.0

// Command github-actions is a knowledge collector for GitHub Actions. It serves
// one MCP tool over stdio; a daemon spawns it, calls that tool with a collect id
// naming a GitHub organization, and writes what it returns into a graph of its
// own.
//
// IT READS AND NEVER WRITES. Every call it makes is a list, and it holds no
// operation that could change anything in an organization.
//
// IT AUTHENTICATES ONLY FROM ITS ENVIRONMENT and takes no credential from its
// caller: there is no flag for one and no argument it reads. The two names it
// consults must be present in the environment block of this collector's own
// config entry, because a stdio child receives exactly what its entry declares
// and nothing else.
//
// STDOUT IS THE PROTOCOL STREAM. Nothing in this program writes to it: every
// diagnostic goes to stderr through the framework, and a stray print anywhere in
// the walk would corrupt the framing and reach an operator as an opaque
// handshake failure with no message worth reading.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/fulminate-io/knowledge-contrib/framework"

	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/ghclients"
	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/walk"
)

func main() {
	// A terminating signal cancels the walk rather than killing it mid-flight,
	// so a partial enumeration is reported as incomplete rather than vanishing.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	collector := walk.Collector{API: ghclients.API}
	if err := framework.ServeStdio(ctx, collector); err != nil {
		// STDERR, and a non-zero exit. The spawning daemon reads both, and a
		// collector that failed silently with status zero would be recorded as a
		// successful collect that found nothing.
		fmt.Fprintf(os.Stderr, "github-actions collector: %v\n", err)
		os.Exit(1)
	}
}
