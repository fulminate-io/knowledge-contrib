// SPDX-License-Identifier: Apache-2.0

// Package logpipe is this collector's own log-processing pipeline: Drain
// clustering, language consolidation, stream fingerprinting, chunk assembly and
// the alias derivation that names the emitted nodes.
//
// WHY IT IS REIMPLEMENTED RATHER THAN IMPORTED. The pipeline the knowledge
// client USED TO run for its built-in logs collect lived under
// cmd/knowledge/internal/collector/logs at a9171723f400f6f4d58fbe6ca27626fb4874c076,
// the last commit before it was deleted, and a module outside that binary could
// not import it — the toolchain refuses an internal path from here, and R1
// of this collector's ticket forbids it in words as well. What crosses is not
// code but a GRAPH VOCABULARY: the node and edge types, their derived ids,
// their metadata keys and the two alias derivers. Every rule below is written
// against that vocabulary, and this package's tests assert the derived values
// literally rather than round-tripping them through this package's own
// encoders, because a self-consistent reimplementation of the wrong rule passes
// a round trip and produces a graph that stores, reads back and is wrong.
//
// THE UNITS BELOW ARE NOT THE CLIENT'S TYPES. They are this module's own carrier
// structs, deliberately narrow: nothing here is a wire type, and the only thing
// that leaves this package is the framework's node and edge shape (see emit.go).
package logpipe

import "time"

// Entry is one log line with its labels, as this collector's Loki reader hands
// it to the pipeline.
type Entry struct {
	// Timestamp is when the log line was emitted.
	Timestamp time.Time
	// Severity is the canonical log level (see severity.go).
	Severity string
	// Message is the log line text, after JSON message extraction.
	Message string
	// Labels are the stream's labels, copied onto every entry.
	Labels map[string]string
}

// Template is a Drain-clustered log pattern: entries sharing a structure map to
// one template whose variable tokens are replaced by the wildcard.
type Template struct {
	// ID is the first 16 bytes of sha256(Pattern), hex-rendered (32 hex chars).
	// It MOVES when the pattern is broadened by a merge; see drain.go.
	ID string
	// Pattern is the template text with wildcards in the variable positions.
	Pattern string
	// Severity is the highest severity observed across the matching entries.
	Severity string
	// Count is how many entries matched.
	Count int
	// FirstSeen and LastSeen bound the matching entries.
	FirstSeen time.Time
	LastSeen  time.Time
	// ExampleVars holds up to maxExampleVars variable rows from recent matches.
	ExampleVars [][]string
	// Alias is derived from Pattern and Severity by TemplateAliasFor, and is
	// RECOMPUTED on every merge because both inputs move.
	Alias string
}

// Stream is a unique label set. Its identity is the hash of the FULL label set;
// its fingerprint is the hash of the low-cardinality subset only.
type Stream struct {
	// ID is the hex sha256 of the sorted full label set (64 hex chars).
	ID string
	// Labels is the complete label set.
	Labels map[string]string
	// LowCardLabels and HighCardLabels are the cardinality split of Labels.
	// The split is COLLECT-SCOPED: it is computed over this walk's entries
	// alone, so a label can change sides between two collects while the
	// stream id does not move.
	LowCardLabels  map[string]string
	HighCardLabels map[string]string
	// Fingerprint is the hex sha256 of the sorted low-cardinality labels.
	Fingerprint string
	// Alias is derived from the label set by AliasFor — a DIFFERENT deriver
	// from the template's, with different character rules.
	Alias string
}

// Chunk is a time-bounded block of entries sharing one stream and one template.
// Data holds the zstd frame; the encoded-then-compressed layout is chunk.go's.
type Chunk struct {
	ID         string
	StreamID   string
	TemplateID string
	StartTime  time.Time
	EndTime    time.Time
	// Data is the ZSTD-compressed entry frame. It becomes the emitted node's
	// Content verbatim.
	Data       []byte
	EntryCount int
}

// Resolution is one label-to-cloud-resource mapping the collect input supplies.
// It is the input the proxy emitter consumes; this collector never resolves a
// cloud resource itself, because it holds no cloud session and no cloud graph.
type Resolution struct {
	LabelKey   string
	LabelValue string
	Account    string
	ResourceID string
}
