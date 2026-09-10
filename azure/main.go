// SPDX-License-Identifier: Apache-2.0

// Command azure is the greenfield knowledge collector for an Azure
// subscription. It serves one MCP tool over stdio through the collector
// framework and writes its own custom graph family.
//
// It is installed by writing a config-file entry naming this binary, its tool
// and the environment its credential chain needs; see README.md for the worked
// entry and what every variable in it does.
//
// STDOUT IS THE PROTOCOL STREAM. Every diagnostic this process writes goes to
// stderr, because anything printed to stdout corrupts the JSON-RPC framing and
// reaches an operator as an opaque handshake failure.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/fulminate-io/knowledge-contrib/framework"

	"github.com/fulminate-io/knowledge-contrib/azure/internal/azure"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := framework.ServeStdio(ctx, azure.New()); err != nil {
		fmt.Fprintf(os.Stderr, "azure collector: %v\n", err)
		os.Exit(1)
	}
}
