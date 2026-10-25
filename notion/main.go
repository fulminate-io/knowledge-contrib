// SPDX-License-Identifier: Apache-2.0

// Command notion is a knowledge collector for Notion. It serves one MCP tool
// over stdio: a collect names pages, databases, data sources or title
// searches, and the tool returns the pages with their block text, the
// databases and data sources with their entries, and the unresolved comments
// it read through the Notion API with the token in NOTION_API_KEY.
//
// Every call it makes is a read. Stdout carries only protocol frames; every
// diagnostic goes to stderr.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/fulminate-io/knowledge-contrib/framework"

	"github.com/fulminate-io/knowledge-contrib/notion/internal/walk"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := framework.ServeStdio(ctx, walk.Collector{}); err != nil {
		fmt.Fprintf(os.Stderr, "notion collector: %v\n", err)
		os.Exit(1)
	}
}
