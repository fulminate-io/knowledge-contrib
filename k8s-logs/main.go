// SPDX-License-Identifier: Apache-2.0

// Command k8s-logs is the Kubernetes pod-and-container log collector: an MCP
// provider serving one collect tool over stdio, which reads pod logs through
// the standard kubeconfig or in-cluster credentials and returns a log graph of
// streams, Drain-clustered templates, compressed chunks and shared label nodes.
//
// # Architecture rationale (a workspace module of its own)
//
// This repository holds a hard lock against new modules, so a new one owes an
// argument. This one's is short and structural: THE COLLECTOR CONTRACT IS MCP
// OVER STDIO. A custom collector is a separate PROCESS the daemon spawns and
// speaks JSON-RPC to, which means it is a separate BINARY, which means it is a
// separate main package with its own dependency closure. It could not live
// inside the client module even if that were desirable.
//
// It is desirable independently. This collector pulls in the whole Kubernetes
// client library, and that dependency is the reason the client module is
// excluded from the repository's static-analysis matrix today — the analyzer's
// memory goes from under two gigabytes to nearly eight on those packages. Every
// collector that moves out takes its SDK with it.
//
// # What this binary must never do
//
// STDOUT IS THE PROTOCOL STREAM. In stdio mode this process speaks
// newline-delimited JSON-RPC on stdin and stdout, so any write to stdout that
// is not a protocol message corrupts the framing, and an operator sees an
// opaque handshake failure rather than an error. Every diagnostic this binary
// writes goes to stderr. The serving layer is the framework's, but that rule
// binds this module all the same: no framework can stop this module's own code
// from printing.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/fulminate-io/knowledge-contrib/framework"
	"github.com/fulminate-io/knowledge-contrib/k8s-logs/internal/k8slogs"
)

func main() {
	// Signals are handled so an operator's interrupt ends the session and the
	// in-flight walk rather than killing the process mid-read: a cancelled
	// context reaches every apiserver stream this collector holds open.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := framework.ServeStdio(ctx, &k8slogs.Collector{}); err != nil {
		// STDERR, never stdout: see this command's doc comment.
		fmt.Fprintf(os.Stderr, "k8s-logs: %v\n", err)
		os.Exit(1)
	}
}
