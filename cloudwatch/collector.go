// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// collector.go — THE COLLECTOR THE FRAMEWORK SERVES.
//
// Everything MCP — the transport, the advertised schemas, the params
// validation, the envelope — belongs to the framework. What lives here is the
// walk: get a client, read the log groups, build the graph, and say whether the
// walk enumerated its source.

// toolName is the MCP tool this collector serves. It is the contract's default
// name, so an operator's config entry needs no `tool` field.
const toolName = "collect"

// Collector walks CloudWatch log groups.
type Collector struct {
	// newClient builds the CloudWatch client for a region. It is a field
	// rather than a direct call so a test drives the walk against recorded
	// pages with no credential and no network; production leaves it nil and
	// gets the default-chain client.
	newClient func(ctx context.Context, region string) (filterLogEventsClient, error)
}

// New returns a collector that reads AWS credentials from the default chain.
func New() *Collector { return &Collector{} }

// Tool names the MCP tool this collector serves.
func (c *Collector) Tool() framework.ToolSpec {
	return framework.ToolSpec{
		Name: toolName,
		Description: "Walk AWS CloudWatch log groups over a time window and return the log graph: " +
			"clustered message templates, label streams, per-window entry chunks and shared label nodes. " +
			"Credentials come from the AWS default credential chain.",
	}
}

// Walk reads the named log groups and returns the graph they produce.
//
// THE COLLECT ID IS DELIBERATELY UNUSED. It names the graph INSTANCE this
// result lands in, which the client decides; nothing derived from it may enter
// a node id, or two instances collecting the same log group would produce
// different ids for the same events and never carry anything forward.
//
// THE FOREIGN CONTEXT IS THE CLOUD SIDE, and it arrives per call rather than on
// this value: the client fills the block from the graphs this collector's entry
// declares, and hands it over as an argument. An EMPTY block is the ordinary
// case and emits no proxy, no EMITTED_BY and no CORRELATES_WITH, which is what
// the built-in path does with no cloud graph attached.
//
// THE ERROR ARMS WRITE NOTHING. A refusal here becomes a tool-call error and
// the collect writes no graph at all, which is the whole-or-nothing behavior
// the contract documents. The alternative — returning what was read so far with
// a complete assertion — would tell the deletion phase that everything the
// failed walk missed is gone.
func (c *Collector) Walk(
	ctx context.Context, _ string, params Params, foreign framework.ForeignContext,
) (framework.Result, error) {
	w, err := params.validate()
	if err != nil {
		return framework.Result{}, err
	}

	client, err := c.clientFor(ctx, params.Region)
	if err != nil {
		return framework.Result{}, err
	}

	fetched, err := fetchAll(ctx, client, params, w)
	if err != nil {
		return framework.Result{}, err
	}

	nodes, edges, err := buildGraph(fetched.Entries, cloudContextFrom(foreign))
	if err != nil {
		return framework.Result{}, err
	}

	complete := framework.Complete()
	if fetched.Truncated {
		complete = framework.Incomplete(fetched.Reason)
	}
	return framework.Result{Nodes: nodes, Edges: edges, Complete: complete}, nil
}

// clientFor builds the CloudWatch client for this collect, through the
// collector's own constructor when one was installed.
func (c *Collector) clientFor(ctx context.Context, region string) (filterLogEventsClient, error) {
	if c.newClient != nil {
		return c.newClient(ctx, region)
	}
	client, err := newDefaultChainClient(ctx, region)
	if err != nil {
		return nil, err
	}
	return client, nil
}
