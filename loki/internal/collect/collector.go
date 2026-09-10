// SPDX-License-Identifier: Apache-2.0

// Package collect is the collector this module serves: the params its tool
// accepts, and the walk that reads a Loki window and returns a log graph.
package collect

import (
	"context"
	"fmt"
	"time"

	"github.com/fulminate-io/knowledge-contrib/framework"
	"github.com/fulminate-io/knowledge-contrib/loki/internal/logpipe"
	"github.com/fulminate-io/knowledge-contrib/loki/internal/lokiapi"
)

// collector.go — the framework.Collector this module serves.
//
// THE DIVISION OF LABOR. The framework owns MCP, the two advertised contract
// schemas, the envelope and the transport; this module owns the PARAMS schema
// and the walk. Everything below the Walk call is this collector's; nothing
// about JSON-RPC, schema inference or the envelope appears in this package.

// ToolName is the tool this collector serves. It is not the framework default:
// an operator installing several collectors reads the tool name in the config
// entry, and "collect_loki_logs" says which one this is.
const ToolName = "collect_loki_logs"

// Params is the collect params this collector accepts, spliced into the
// contract's input schema as the `params` property.
//
// THE ADDRESS IS A PARAMETER, NOT AN ENVIRONMENT VARIABLE, and that is a
// decision rather than an oversight: a collect names which Loki it reads, the
// way the built-in path takes the URL from its backend record. The CREDENTIALS
// are the other way round — they come from the standard Loki client
// environment, and this collector's caller passes none of them.
//
// EVERY FIELD IS DOCUMENTED IN ITS TAG because the tag is what an operator sees
// in a tool listing; a field whose description lives only in this comment is
// undocumented where it is read.
type Params struct {
	// Address is the Loki endpoint. Required.
	Address string `json:"address" jsonschema:"the Loki endpoint to read from, as an absolute http or https URL, for example http://localhost:3100"`
	// Start and End bound the collect window. Required, RFC 3339.
	Start string `json:"start" jsonschema:"the start of the collect window, RFC 3339, for example 2026-09-07T00:00:00Z"`
	End   string `json:"end" jsonschema:"the end of the collect window, RFC 3339; it must be after the start"`

	// Selector is the LogQL stream selector, braces included. When it is empty
	// the selector is built from source and field_filters, and when those name
	// nothing it falls back to every stream carrying a namespace label.
	Selector string `json:"selector,omitempty" jsonschema:"the LogQL stream selector including its braces, for example {app=\"checkout\"}; when empty it is built from source and field_filters"`
	// Source selects one namespace.
	Source string `json:"source,omitempty" jsonschema:"the namespace to read, used only when selector is empty"`
	// FieldFilters are canonical field names mapped onto Loki's label names.
	FieldFilters map[string]string `json:"field_filters,omitempty" jsonschema:"exact-match label filters used only when selector is empty; the canonical names service, host, namespace, pod and container are mapped to this provider's own label names"`
	// TextFilter keeps only entries whose message contains it, case-insensitively.
	TextFilter string `json:"text_filter,omitempty" jsonschema:"keep only entries whose message contains this text, case-insensitively"`
	// SeverityMin keeps only entries at or above a severity.
	SeverityMin string `json:"severity_min,omitempty" jsonschema:"keep only entries at or above this severity: TRACE, DEBUG, INFO, WARN, ERROR or CRITICAL"`
	// RawQuery is appended to the built LogQL verbatim.
	RawQuery string `json:"raw_query,omitempty" jsonschema:"LogQL appended to the built query verbatim, for the stages the structured fields cannot express"`
}

// resolved is Params after parsing and validation.
type resolved struct {
	address string
	start   time.Time
	end     time.Time
	query   lokiapi.Query
}

// validate parses every param and refuses the ones that are not what they say.
//
// IT RUNS BEFORE ANY REQUEST IS BUILT. The knowledge client validates a call's
// arguments against the schema this collector advertised, which enforces that
// the address is a required string and nothing about what the string says, so
// every check below is this collector's own.
func (p Params) validate() (resolved, error) {
	address, err := lokiapi.NormalizeAddress(p.Address)
	if err != nil {
		return resolved{}, err
	}
	start, err := parseWindowBound("start", p.Start)
	if err != nil {
		return resolved{}, err
	}
	end, err := parseWindowBound("end", p.End)
	if err != nil {
		return resolved{}, err
	}
	if !start.Before(end) {
		return resolved{}, fmt.Errorf(
			"the collect window starts at %s and ends at %s; start must be before end",
			p.Start, p.End)
	}
	severity, err := normalizeSeverityMin(p.SeverityMin)
	if err != nil {
		return resolved{}, err
	}
	return resolved{
		address: address,
		start:   start,
		end:     end,
		query: lokiapi.Query{
			Selector:     p.Selector,
			Source:       p.Source,
			FieldFilters: p.FieldFilters,
			TextFilter:   p.TextFilter,
			SeverityMin:  severity,
			RawQuery:     p.RawQuery,
		},
	}, nil
}

// parseWindowBound reads an RFC 3339 timestamp, naming the field and the value.
// An empty bound is refused rather than defaulted to "now" or "the epoch": a
// window with an implied end is a different collect every time it runs, and the
// graph it lands in is keyed on the collect id rather than on the window.
func parseWindowBound(field, value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, fmt.Errorf("the collect window's %q bound is empty; both bounds are required, as RFC 3339 timestamps", field)
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("the collect window's %q bound %q is not an RFC 3339 timestamp: %w", field, value, err)
	}
	return t, nil
}

// normalizeSeverityMin refuses a severity outside the canonical vocabulary.
// ParseSeverity maps an unknown level to INFO, which is right for a log line
// and wrong for a filter: a caller who wrote "ERR0R" would silently get every
// entry at INFO and above.
func normalizeSeverityMin(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	switch value {
	case logpipe.SeverityTrace, logpipe.SeverityDebug, logpipe.SeverityInfo,
		logpipe.SeverityWarn, logpipe.SeverityError, logpipe.SeverityCritical:
		return value, nil
	default:
		return "", fmt.Errorf(
			"severity_min %q is not a severity; use one of TRACE, DEBUG, INFO, WARN, ERROR or CRITICAL",
			value)
	}
}

// Collector reads a Loki window and returns its log graph.
type Collector struct {
	// Lookup reads the environment the credentials and transport come from. It
	// is a field so a test can supply one without mutating the process's
	// environment; a nil value reads the real one.
	Lookup lokiapi.LookupFunc
	// Options tunes the pipeline. The zero value takes every default, which is
	// what the binary uses.
	Options logpipe.Options
}

// Tool names the served tool.
func (c *Collector) Tool() framework.ToolSpec {
	return framework.ToolSpec{
		Name: ToolName,
		Description: "Collect logs from a Grafana Loki endpoint over a time window and return them as a log graph " +
			"of templates, streams, chunks and labels. Credentials and transport settings come from the standard " +
			"Loki client environment (LOKI_USERNAME, LOKI_BEARER_TOKEN, LOKI_ORG_ID and the rest); the endpoint " +
			"address is a parameter of this call.",
	}
}

// Walk reads the window and returns the graph.
//
// THE COLLECT ID IS NOT USED TO DERIVE ANYTHING. It names the graph INSTANCE
// the result lands in, which is the client's business; this collector emits the
// same nodes for the same window whatever the collect id is, which is what
// makes a second collect under one id a diff rather than a new graph.
//
// THE FOREIGN CONTEXT IS THE ONE INPUT THIS COLLECTOR CANNOT COMPUTE. It
// carries the cloud graphs this collector's registration entry declared it
// needs, read out of the operator's own graphs by the client; the log-to-cloud
// resolutions are derived from it here, and the proxy nodes and EMITTED_BY
// edges are emitted from those. An entry that declares nothing yields the zero
// value and this walk resolves nothing, which is what the built-in path does
// with no cloud graph attached.
func (c *Collector) Walk(ctx context.Context, _ string, params Params, foreign framework.ForeignContext) (framework.Result, error) {
	r, err := params.validate()
	if err != nil {
		return framework.Result{}, err
	}
	lookup := c.Lookup
	if lookup == nil {
		lookup = lokiapi.OSLookup
	}
	settings, err := lokiapi.LoadSettings(lookup)
	if err != nil {
		return framework.Result{}, err
	}
	client, err := lokiapi.NewClient(r.address, settings)
	if err != nil {
		return framework.Result{}, err
	}

	read, err := client.Walk(ctx, r.query, r.start, r.end)
	if err != nil {
		return framework.Result{}, err
	}
	graph, err := logpipe.Build(read.Entries, c.Options)
	if err != nil {
		return framework.Result{}, err
	}

	// THE CLOUD CONTEXT IS BUILT ONCE and answers both cloud questions, so the
	// resource a proxy names and the resource a correlation's evidence names
	// cannot disagree. A collect whose entry declares no provider family gets a
	// nil context, resolves nothing and confirms nothing.
	//
	// EVERY DECLARED FAMILY BUT code, flattened, is what the resolver takes. The
	// block used to carry a `Cloud` field and now it is keyed by family, because
	// there is no built-in cloud type: inventory is collected by contrib
	// collectors, each registering its OWN graph type, so a declaration names the
	// providers an operator installed and this collector cannot know their names
	// ahead of time. Naming what to EXCLUDE is what keeps that open — a list of
	// families to include would need revising every time an operator registers a
	// provider, and a stale one drops resolutions silently rather than failing.
	cloud := logpipe.NewCloudContext(foreign.Except(framework.FamilyCode))

	// THE RESOLUTIONS drive the proxy nodes and the EMITTED_BY edges.
	resolutions := logpipe.ResolutionsFromContext(graph.Streams, cloud)

	// THE CORRELATIONS come from the common detector, which every logs
	// collector shares. Its confirmation half asks whether the two owning
	// resources depend on each other, which only the operator's cloud graph
	// knows and which arrives in the same declared block; only a confirmed pair
	// becomes an edge, so the graph never carries a coincidence under the same
	// edge type as a dependency.
	//
	// A DETECTOR ERROR FAILS THE WALK. It reports malformed input — a nil
	// template, an empty id, a range running backwards — which is this
	// collector's own pipeline contradicting itself, not a source it can
	// tolerate.
	//
	// NO TEST REDS ON REMOVING THIS RETURN, and that is stated rather than left
	// for a reviewer to measure: the pipeline this walk runs cannot produce any
	// of those shapes, so the branch is unreachable from here. It is kept
	// because the detector's contract permits the error and swallowing a
	// contract's error arm is how the next change to the pipeline becomes a
	// silent wrong answer. The propagation itself IS observed one layer down,
	// where the error is reachable: TestMalformedPipelineOutputFailsTheWalk in
	// the logpipe package reds when the adapter swallows it.
	correlations, err := logpipe.FindCorrelations(graph, resolutions, cloud)
	if err != nil {
		return framework.Result{}, err
	}

	nodes, edges, err := logpipe.Emit(graph, resolutions, correlations)
	if err != nil {
		return framework.Result{}, err
	}

	complete := framework.Complete()
	if !read.Complete {
		complete = framework.Incomplete(read.Incomplete)
	}
	return framework.Result{Nodes: nodes, Edges: edges, Complete: complete}, nil
}
