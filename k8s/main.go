// SPDX-License-Identifier: Apache-2.0

// Command k8s is a Kubernetes collector for the knowledge graph, served as an
// MCP provider over stdio.
//
// It enumerates a cluster's objects and the relationships between them, and
// returns them as one collect result. It reads a cluster and never writes to
// one: every API call it makes is a list.
//
// CREDENTIALS COME FROM THE ENVIRONMENT, NOT FROM THE CALLER. The caller passes
// a collect id and, optionally, a kubeconfig context name; the credential is
// resolved by the standard Kubernetes rules from the variables the operator's
// config entry declared. See README.md for the entry, and env.go for the list.
//
// STDOUT IS THE PROTOCOL STREAM. Anything written to stdout that is not a
// JSON-RPC frame corrupts the framing and reaches an operator as an opaque
// handshake failure, so every diagnostic this program emits goes to stderr —
// which is where log/slog's default handler writes, and why nothing here
// prints.
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
	// The signal context is what turns an operator's interrupt, or the
	// daemon closing the child down, into a cancelled walk that returns
	// promptly rather than a process killed part way through a listing.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	c := &k8sCollector{sources: defaultCredentialSources()}
	if err := framework.ServeStdio(ctx, c); err != nil {
		fmt.Fprintf(os.Stderr, "k8s collector: %v\n", err)
		os.Exit(1)
	}
}
