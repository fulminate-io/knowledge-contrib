// SPDX-License-Identifier: Apache-2.0

// Package logpipe turns raw log entries into the log graph's vocabulary: Drain
// template clustering, cardinality classification, stream fingerprinting,
// chunking, alias derivation, consolidation, and the node and edge set the
// collector contract carries.
//
// WHY THIS IS A REIMPLEMENTATION AND NOT AN IMPORT. The knowledge client
// USED TO contain this pipeline, at cmd/knowledge/internal/collector/logs read
// at a9171723f400f6f4d58fbe6ca27626fb4874c076, the last commit before the
// built-in log collectors were deleted. That path was under an `internal`
// directory of a DIFFERENT module, so the Go toolchain refused the import
// outright — not a style preference, a compile error, and the reimplementation
// stands whether or not the original is still there. The ticket that created
// this module settled the consequence
// explicitly: parity with the built-in log graph is a GRAPH-COVERAGE target,
// never code reuse. So every derived property below is reproduced to the byte,
// and the tests assert the derived values rather than the code shape.
//
// THE DERIVED PROPERTIES ARE THE CONTRACT, NOT THE TYPE STRINGS. An
// implementation can emit every type string correctly and still build a
// structurally different graph. The ten properties that decide the shape are:
// the template id (a TRUNCATED sha256 of the Drain pattern alone), the stream
// id (a FULL sha256 over all labels), the stream fingerprint (the same hash
// over the LOW-CARDINALITY labels only), the label node id (a literal, not a
// hash), the chunk id (a prefixed truncated hash over two ids and a
// big-endian window start), the chunk window (floored to a bucket aligned on
// the UTC epoch, never on the first entry), the cardinality class (which
// decides whether a label becomes a node at all), the stream alias, the
// template alias (recomputed every time a cluster absorbs an entry and the
// pattern broadens), and the consolidated template SET.
//
// ALIASES ARE NOT UNIQUE and this package does not pretend otherwise. Two
// streams whose two lowest-sorting label keys agree derive the same alias. The
// built-in reader resolves that at query time by appending a short hash suffix;
// a custom graph read through generic search has no such layer. The emitted
// LABEL KEY NAMES are therefore chosen so the alias discriminates — see
// [k8slogs.Labels] in the collector package for the naming and its reason.
package logpipe
