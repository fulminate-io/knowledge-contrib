// SPDX-License-Identifier: Apache-2.0

package main

import "time"

// entry.go — the DOMAIN TYPES this module owns, and why it owns them.
//
// The knowledge client USED TO carry the same five shapes under
// cmd/knowledge/internal/logwire at a9171723f400f6f4d58fbe6ca27626fb4874c076,
// the last commit before the built-in log collectors and that package were
// deleted. That package was client-internal and the toolchain refused to import
// it from another module, which was the ticket's requirement rather than an
// accident: this collector is a greenfield reimplementation of a produce path,
// not a second caller of it. It is now the only producer of these shapes. The
// types below are therefore declared here, and the parity that matters is the
// EMITTED GRAPH — ids, metadata keys, edge types — not the Go shapes.
//
// NOTHING HERE CROSSES THE WIRE. The contract envelope is framework.Node and
// framework.Edge; these are the intermediate values the walk builds before
// graph.go turns them into it.

// logEntry is a single normalized log line with its label set.
type logEntry struct {
	// Timestamp is when the line was emitted, as the provider reported it.
	Timestamp time.Time
	// Severity is one of the six canonical severities.
	Severity string
	// Message is the extracted human-readable message.
	Message string
	// Labels is the full label set. EVERY key here enters the stream id, so a
	// key added by mistake changes the id of every stream that carries it.
	Labels map[string]string
}

// logTemplate is a Drain-clustered pattern. Entries that share a structure map
// to one template whose variable positions carry the wildcard token.
type logTemplate struct {
	// ID is the truncated sha256 of Pattern. It moves whenever Pattern does.
	ID string
	// Pattern is the clustered template text.
	Pattern string
	// Severity is the HIGHEST severity any member entry carried.
	Severity string
	// Count is how many entries matched.
	Count int
	// FirstSeen and LastSeen bound the matched entries.
	FirstSeen time.Time
	LastSeen  time.Time
	// ExampleVars holds up to maxExampleVars rows of the variable values seen
	// at the wildcard positions.
	ExampleVars [][]string
	// Alias is the readable identifier derived from Pattern and Severity. It is
	// re-derived on every update, because both inputs move.
	Alias string
}

// logStream is one unique label set. Streams are the grouping key entries are
// bucketed under and the anchor the label nodes hang off.
type logStream struct {
	// ID is the fingerprint over the FULL label set.
	ID string
	// Labels is the full label set.
	Labels map[string]string
	// LowCardLabels are the labels whose key stayed under the cardinality
	// threshold across the whole collect. They become shared label nodes.
	LowCardLabels map[string]string
	// HighCardLabels are the rest. They stay inline on the stream node.
	HighCardLabels map[string]string
	// Fingerprint is the fingerprint over the LOW-CARDINALITY labels only, so
	// two streams differing on a high-cardinality label share it.
	Fingerprint string
	// Alias is the readable identifier derived from the label set.
	Alias string
}

// logChunk is a time-bounded block of entries for one (stream, template,
// window) bucket, compressed.
type logChunk struct {
	ID         string
	StreamID   string
	TemplateID string
	StartTime  time.Time
	EndTime    time.Time
	// CompressedData is the zstd-compressed varint entry stream. graph.go is
	// where it acquires its wire encoding; see the note there on why the
	// envelope cannot carry it raw.
	CompressedData []byte
	EntryCount     int
}

// resolvedProxyEntry is one (log label, cloud resource) resolution: the input
// from which a proxy node and its EMITTED_BY edge are built.
//
// A COLLECTOR CANNOT DERIVE THESE FROM ITS OWN SOURCE. Which log label names
// which cloud resource is a fact about the operator's cloud graph, which this
// process cannot read; the values arrive as declared foreign-graph context on
// the collect input. See resolve.go for what this module does with them and
// what it cannot do yet.
type resolvedProxyEntry struct {
	LabelKey   string
	LabelValue string
	Account    string
	ResourceID string
}
