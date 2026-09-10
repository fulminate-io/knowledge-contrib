// SPDX-License-Identifier: Apache-2.0

// Command aws is the AWS collector: an MCP server exposing one tool that
// enumerates an AWS account's resources and the relationships among them, as the
// collector contract's nodes and edges.
//
// IT IS INSTALLED AS A CONFIG ENTRY, not registered through a tool call. An entry
// in ~/.knowledge/collectors.json (or a repository's own .knowledge/collectors.json)
// names this binary, the tool below, and the environment the daemon spawns it
// with; `knowledge collector add` writes one. The ENTRY NAME is the graph family
// the results land in, and the collect id is the graph instance — conventionally
// the AWS account id.
//
// STDIO IS THE TRANSPORT. The daemon spawns this binary and speaks MCP over its
// stdin and stdout, so STDOUT IS THE PROTOCOL STREAM: every diagnostic this
// process writes goes to stderr, and a stray print to stdout corrupts the JSON-RPC
// framing into an opaque handshake failure. The framework owns the transport;
// this file owns the process.
//
// CREDENTIALS COME FROM THE AWS DEFAULT CHAIN and this binary takes none of its
// own: no flag, no argument and no file of its own carries a credential. What the
// chain can see is exactly what the entry's `env` block supplies, because the
// daemon gives a spawned collector that block as its WHOLE environment and copies
// nothing from its own. README.md carries the block this collector needs and what
// each name in it does.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/fulminate-io/knowledge-contrib/framework"

	"github.com/fulminate-io/knowledge-contrib/aws/internal/awswalk"
)

func main() {
	// SIGNALS END THE WALK RATHER THAN THE PROCESS. A daemon shutting a
	// collector down sends a signal, and a walk cancelled mid-flight must return
	// an INCOMPLETE result rather than a killed process — a killed process is a
	// failed collect that writes nothing, which is correct, but a cancelled walk
	// that reported a complete enumeration would arm the server's deletion phase
	// over everything it never reached.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := framework.ServeStdio(ctx, awswalk.New()); err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintf(os.Stderr, "aws collector: %v\n", err)
		os.Exit(1)
	}
}
