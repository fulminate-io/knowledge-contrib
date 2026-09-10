// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// collector.go — the COLLECTOR the binary serves: the tool it advertises and
// the walk it runs.

// toolName is the MCP tool this collector serves. It matches the `tool` value
// in the collector config entry's own worked example, so an operator installing
// this collector writes the name the framework's default already uses.
const toolName = "collect"

// entryReader performs one read of Cloud Logging. It is a function type on the
// collector rather than a package-level call so the WHOLE walk — the pipeline,
// the graph assembly and the completeness assertion — is exercised against
// recorded entries with no network and no credential.
//
// THE SEAM IS AT THE READ AND NOT AT THE CLIENT deliberately. Faking the SDK's
// concrete client type is not possible, and faking one layer lower, at the
// entries iterator, would leave the bound and the truncation assertion outside
// what a test of the walk covers — those are exactly the behaviors that decide
// whether the server may treat missing rows as deleted.
type entryReader func(ctx context.Context, projectID string, q logQuery) (drainResult, error)

// Collector serves the Cloud Logging walk.
type Collector struct {
	read   entryReader
	config pipelineConfig
}

// New builds the collector the binary serves, reading Cloud Logging for real.
func New() *Collector {
	return &Collector{read: readCloudLogging, config: defaultPipelineConfig()}
}

// Tool names the MCP tool and describes it for an operator reading a tool list.
func (c *Collector) Tool() framework.ToolSpec {
	return framework.ToolSpec{
		Name: toolName,
		Description: "Read Google Cloud Logging entries for one project and return them as a log graph: " +
			"clustered templates, label streams, compressed entry chunks and shared labels. " +
			"Credentials come from Application Default Credentials only.",
	}
}

// Walk performs one collect.
//
// foreign is the DECLARED FOREIGN-GRAPH CONTEXT: the cloud resources this
// collector's config entry declared it needs, read out of the operator's own
// graphs by the client and sent with the call. It is what lets the walk resolve
// its log labels to cloud resources and emit the proxy nodes, the EMITTED_BY
// edges and the confirmed CORRELATES_WITH edges — none of which this process
// could derive from its own source, because which log label names which cloud
// resource is a fact about a graph it cannot read.
//
// AN ENTRY THAT DECLARES NOTHING GETS THE ZERO VALUE, and the walk then emits
// none of the three and succeeds. That is the honest result rather than a
// degraded one: nothing was dropped and no resolution was attempted and
// abandoned; there was nothing to resolve against.
func (c *Collector) Walk(
	ctx context.Context, id string, p params, foreign framework.ForeignContext,
) (framework.Result, error) {
	query, err := p.toQuery()
	if err != nil {
		return framework.Result{}, err
	}
	read, err := c.read(ctx, p.Project, query)
	if err != nil {
		return framework.Result{}, err
	}
	// EVERY DECLARED FAMILY BUT code, flattened. This collector matches a log
	// stream against resource NAMES and metadata, not against a provider: which
	// provider graph a resource came from is not a fact it reads. Naming the
	// family to exclude rather than the ones to include is what keeps it
	// correlating against a provider registered after it was written.
	out, err := runPipeline(read.Entries, c.config,
		newForeignCloudContext(foreign.Except(framework.FamilyCode)))
	if err != nil {
		return framework.Result{}, err
	}
	result, err := buildResult(out, read.Truncated, query.MaxEntries)
	if err != nil {
		return framework.Result{}, fmt.Errorf("stackdriver: building the result for collect %s: %w", id, err)
	}
	return result, nil
}

// compile-time assertion that this collector satisfies the framework's
// interface at the params type it declares, so a signature drift surfaces here
// rather than as a type-inference error at the ServeStdio call.
var _ framework.Collector[params] = (*Collector)(nil)
