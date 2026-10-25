// SPDX-License-Identifier: Apache-2.0

// Command slack is a knowledge collector for Slack. It serves one MCP tool
// over stdio: a collect names channels, threads or search queries, and the
// tool returns the channels, messages, thread replies and posting users it
// read through the Slack Web API with the token in SLACK_TOKEN.
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

	"github.com/fulminate-io/knowledge-contrib/slack/internal/walk"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := framework.ServeStdio(ctx, walk.Collector{}); err != nil {
		fmt.Fprintf(os.Stderr, "slack collector: %v\n", err)
		os.Exit(1)
	}
}
