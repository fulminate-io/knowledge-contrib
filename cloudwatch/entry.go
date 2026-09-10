// SPDX-License-Identifier: Apache-2.0

// Package cloudwatch is the AWS CloudWatch Logs custom knowledge collector: an
// MCP provider that walks one or more CloudWatch log groups over a bounded time
// window and returns the log graph as the collector contract's node and edge
// envelope.
//
// WHAT IT PRODUCES is the log vocabulary the built-in log pipeline produces for
// the same events — the same four node types, the same three edge types, and,
// when the collect supplies cloud resolutions, the proxy node and the two
// cross-graph-flavored edges the built-in materializer builds. The ids are
// derived by the same rules, so the same events yield byte-identical ids: that
// is what makes a re-collect carry the unchanged nodes forward instead of
// replacing the graph.
//
// WHY IT IS REPRODUCED RATHER THAN IMPORTED. The built-in pipeline lives under
// cmd/knowledge/internal and the Go toolchain refuses that import from another
// module. The vocabulary is the contract; the code is not shared, and every
// derivation rule this package implements names the rule it reproduces.
//
// STDOUT IS THE PROTOCOL STREAM. This collector serves MCP over stdin and
// stdout; anything else written to stdout corrupts the JSON-RPC framing and
// reaches the operator as an opaque handshake failure. Every diagnostic goes to
// stderr.
package main

import "time"

// entry.go — the VALUE TYPES this collector's stages hand to each other.
//
// They are this package's own, not a copy of a wire type: nothing about them
// crosses a process boundary. What crosses is the framework's Node and Edge,
// built from these in graph.go.

// LogEntry is one normalized log event: the shape every stage after the
// CloudWatch fetch reads.
type LogEntry struct {
	// Timestamp is when the event was emitted, as CloudWatch reported it.
	// There is no default: an event CloudWatch returns without one is an
	// error, because the timestamp is floored into a chunk window and hashed
	// into that chunk's id, so substituting a wall clock would move the id
	// between two collects of the same events.
	Timestamp time.Time
	// Severity is the canonical level, one of the severity.go constants.
	Severity string
	// Message is the message text after the wrappers are unpeeled.
	Message string
	// Labels identify the source of the event. For CloudWatch they are
	// log_group, service and log_stream; see normalize.go for each one's rule.
	Labels map[string]string
}

// LogTemplate is one Drain cluster: a message skeleton with its variable
// positions replaced by the wildcard, plus the aggregates over the entries that
// clustered into it.
type LogTemplate struct {
	// ID is sha256 of Pattern truncated to 16 BYTES and rendered hex, so 32
	// hex characters. It MOVES whenever Pattern broadens.
	ID string
	// Pattern is the clustered skeleton.
	Pattern string
	// Severity is the MAXIMUM severity over the cluster's entries by
	// severityRank — not the first entry's, not the last's, not the modal one.
	Severity string
	// Count is how many entries clustered here.
	Count int
	// FirstSeen and LastSeen are the minimum and maximum entry timestamps.
	FirstSeen time.Time
	LastSeen  time.Time
	// ExampleVars holds a few captured variable rows, which the consolidators
	// read to recognize a stack-trace fragment whose Pattern alone is wildcards.
	ExampleVars [][]string
	// Alias is the readable identifier derived from Pattern and Severity. It
	// moves with the pattern, and it is this template's node SymbolName.
	Alias string
}

// LogStream is one unique label set: the source identity entries are grouped by.
type LogStream struct {
	// ID is the sha256 of the FULL label set, rendered whole — 64 hex
	// characters, not truncated like a template or chunk id.
	ID string
	// Labels is the complete label set.
	Labels map[string]string
	// LowCardLabels are the labels whose keys stayed under the cardinality
	// threshold across the whole collect. They become shared label nodes.
	LowCardLabels map[string]string
	// HighCardLabels are the rest. They stay inline on the stream node and get
	// no node and no edge of their own.
	HighCardLabels map[string]string
	// Fingerprint is a SECOND hash over the LOW-CARD labels only, so two
	// streams sharing their low-card labels share a fingerprint while their
	// ids differ. Conflating it with ID is the easy error.
	Fingerprint string
	// Alias is the readable identifier derived from the label set, and this
	// stream's node SymbolName.
	Alias string
}

// LogChunk is the entries of one (stream, template, time window) group.
type LogChunk struct {
	// ID is "log-chunk:" followed by 32 hex characters; see chunkID.
	ID string
	// StreamID and TemplateID name the two nodes this chunk hangs off.
	StreamID   string
	TemplateID string
	// StartTime and EndTime are the minimum and maximum timestamps of the
	// entries IN THIS CHUNK — never the window's own edges, which are wider.
	StartTime time.Time
	EndTime   time.Time
	// Content is the rendered entry block; see encodeChunkContent for the
	// format and for why it is text rather than a compressed block.
	Content string
	// EntryCount is the number of entries in this chunk, which is not the
	// template's Count and not the collect's total.
	EntryCount int
}

// ResolvedProxy is one label-to-cloud-resource resolution: the input the proxy
// nodes and EMITTED_BY edges are built from.
type ResolvedProxy struct {
	// LabelKey and LabelValue identify the log label node the edge starts at.
	LabelKey   string
	LabelValue string
	// Account and ResourceID identify the cloud-graph node the proxy stands
	// for, and together they determine the proxy's id.
	Account    string
	ResourceID string
}
