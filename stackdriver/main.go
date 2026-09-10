// SPDX-License-Identifier: Apache-2.0

// Command stackdriver is the knowledge collector for Google Cloud Logging,
// formerly Stackdriver. It serves one MCP tool over stdin and stdout that reads
// a project's log entries and returns them as a graph of log streams, clustered
// templates, compressed entry chunks and shared labels.
//
// WHAT IT IS NOT: a second caller of the knowledge client's own log pipeline.
// That pipeline lives under cmd/knowledge/internal and is unimportable from
// here by construction; this module reimplements the produce path so a collector
// is a program an operator can install, read and replace rather than a branch
// inside the daemon. Where a derivation here matches that pipeline's, it matches
// because the EMITTED GRAPH has to reconcile: the ids, the metadata keys and the
// edge types are a contract with whatever reads the graph back.
//
// CREDENTIALS COME ONLY FROM APPLICATION DEFAULT CREDENTIALS. There is no
// service-account path parameter and no inline credential parameter, and the
// client passes no credential at all. See client.go for what a missing one
// reports and env.go for the names the config entry declares so ADC can find
// one.
//
// STDOUT IS THE PROTOCOL STREAM. Every diagnostic this program writes goes to
// stderr; a stray byte on stdout is a corrupt JSON-RPC frame that surfaces as an
// opaque handshake failure.
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
	// The signal context is what turns a shutdown into a clean exit: a daemon
	// stopping a collector closes the pipes and signals, and an unhandled signal
	// would kill the process mid-read with no chance to close the client.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := framework.ServeStdio(ctx, New()); err != nil {
		fmt.Fprintf(os.Stderr, "stackdriver collector: %v\n", err)
		os.Exit(1)
	}
}
