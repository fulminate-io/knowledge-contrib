// SPDX-License-Identifier: Apache-2.0

package logpipe

import "time"

// Entry is one log line with its metadata: the unit the pipeline consumes.
type Entry struct {
	// Timestamp is when the line was emitted, as the source reported it.
	Timestamp time.Time
	// Severity is the log level. The collector derives it from the message
	// body rather than from the container stream it arrived on; see
	// [DeriveSeverity].
	Severity string
	// Message is the raw log text with any transport-added timestamp prefix
	// already stripped.
	Message string
	// Labels identify the source. They decide stream identity, so every key
	// here must be stable across collects.
	Labels map[string]string
}

// Template is a Drain-clustered pattern. Entries sharing a structure map to one
// template with variable tokens replaced by the wildcard.
type Template struct {
	// ID is a TRUNCATED sha256 of Pattern: 32 hex characters, bare. It is
	// recomputed whenever Pattern broadens, so it is not stable within one run
	// until clustering finishes.
	ID string
	// Pattern is the template text with wildcards in the variable positions.
	Pattern string
	// Severity is the highest severity any entry in this cluster carried.
	Severity string
	// Count is how many entries matched.
	Count int
	// FirstSeen and LastSeen bound the cluster's entries in time.
	FirstSeen time.Time
	LastSeen  time.Time
	// ExampleVars holds variable values from a few recent matches, one inner
	// slice per match, capped at [maxExampleVars].
	ExampleVars [][]string
	// Alias is the readable identifier derived from Pattern and Severity.
	Alias string
}

// Stream is one unique label combination: a log source.
type Stream struct {
	// ID is the FULL 64-hex fingerprint of the complete label set.
	ID string
	// Labels is that complete set.
	Labels map[string]string
	// LowCardLabels and HighCardLabels are the split the cardinality tracker
	// made. Only the low-cardinality half becomes shared label nodes.
	LowCardLabels  map[string]string
	HighCardLabels map[string]string
	// Fingerprint is the same hash over the low-cardinality labels alone, so
	// two streams sharing those labels share a fingerprint while keeping
	// distinct ids.
	Fingerprint string
	// Alias is the readable identifier derived from the label set.
	Alias string
}

// Chunk is a time-bounded block of compressed entries for one
// (stream, template, window) bucket. Chunks are the log graph's storage unit.
type Chunk struct {
	// ID carries the "log-chunk:" prefix followed by 32 hex characters.
	ID string
	// StreamID and TemplateID name the two nodes this chunk joins.
	StreamID   string
	TemplateID string
	// StartTime and EndTime are the earliest and latest entry in the chunk,
	// NOT the window bounds.
	StartTime time.Time
	EndTime   time.Time
	// CompressedData is the zstd-compressed timestamp-and-variables stream.
	CompressedData []byte
	// EntryCount is how many entries the chunk holds.
	EntryCount int
}

// Resolution is one confirmed mapping from a log label to a cloud-graph
// resource. It is the input the proxy nodes and their EMITTED_BY edges are
// built from, and it is SUPPLIED to this package rather than computed here: a
// collector process has no graph caller, so the cloud slice reaches it through
// the collect input's declared foreign-graph context block.
type Resolution struct {
	// LabelKey and LabelValue name the log-label node the proxy attaches to.
	LabelKey   string
	LabelValue string
	// Account and ResourceID name the cloud-graph node the proxy stands for.
	Account    string
	ResourceID string
	// ResourceType, Region and Provider are display metadata copied onto the
	// proxy when the supplied cloud node carries them, so a proxy consumer
	// renders without re-resolving against the cloud graph.
	ResourceType string
	Region       string
	Provider     string
}

// Correlation is one candidate pair of error templates whose time ranges
// overlap across two services. Only a StructurallyConfirmed pair becomes an
// edge; an unconfirmed one is a candidate the caller may report but never a
// fact in the graph.
type Correlation struct {
	TemplateA             string
	TemplateB             string
	ServiceA              string
	ServiceB              string
	ResourceA             string
	ResourceB             string
	CooccurrenceScore     float64
	StructurallyConfirmed bool
}
