// SPDX-License-Identifier: Apache-2.0

// Command bitbucket-pipelines is a knowledge collector for Bitbucket Pipelines.
// It serves one MCP tool over stdio; a daemon spawns it, calls that tool with a
// collect id naming a Bitbucket workspace, and writes what it returns into a
// graph of its own.
//
// IT READS AND NEVER WRITES. Every call it makes is a GET, and it holds no
// operation that could change anything in a workspace.
//
// IT AUTHENTICATES ONLY FROM ITS ENVIRONMENT and takes no credential from its
// caller: there is no flag for one and no argument it reads. The two names it
// consults must be present in the environment block of this collector's own
// config entry, because a stdio child receives exactly what its entry declares
// and nothing else.
//
// IT STORES NO VARIABLE VALUE. The variables it enumerates reach the graph as a
// key, a scope and a secured flag; the decoder declares no field for a value, so
// there is no point in this program at which one exists.
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

	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/walk"
)

func main() {
	// A terminating signal cancels the walk rather than killing it mid-flight,
	// so a partial enumeration is reported as incomplete rather than vanishing.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := framework.ServeStdio(ctx, walk.Collector{}); err != nil {
		// STDERR, and a non-zero exit. The spawning daemon reads both, and a
		// collector that failed silently with status zero would be recorded as a
		// successful collect that found nothing.
		fmt.Fprintf(os.Stderr, "bitbucket-pipelines collector: %v\n", err)
		os.Exit(1)
	}
}
