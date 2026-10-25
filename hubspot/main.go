// SPDX-License-Identifier: Apache-2.0

// Command hubspot is a knowledge collector for HubSpot. It serves one MCP
// tool over stdio: a collect names companies, contacts, deals, tickets, lists
// or CRM searches, and the tool returns those records and every record
// associated with them, one hop, read through the HubSpot API with the token
// in HUBSPOT_ACCESS_TOKEN.
//
// Every call it makes is a read (batch reads and searches are POSTs that
// read). Stdout carries only protocol frames; every diagnostic goes to stderr.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/fulminate-io/knowledge-contrib/framework"

	"github.com/fulminate-io/knowledge-contrib/hubspot/internal/walk"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := framework.ServeStdio(ctx, walk.Collector{}); err != nil {
		fmt.Fprintf(os.Stderr, "hubspot collector: %v\n", err)
		os.Exit(1)
	}
}
