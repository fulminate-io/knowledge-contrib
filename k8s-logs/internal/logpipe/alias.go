// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"sort"
	"strings"
	"unicode"
)

// alias.go — the readable identifiers streams and templates carry in
// SymbolName, and the sanitizing rules shared by both derivations.
//
// ALIASES ARE NOT UNIQUE ON THEIR OWN. The built-in reader appends a short hash
// suffix when two streams or two templates would share one; a custom graph read
// through generic search has no such layer, so an alias collision here reaches
// a human as two different sources wearing one name. That is why the collector
// chooses its emitted label KEY NAMES so the derivation below discriminates —
// the mechanism is in this file, the choice is in the collector package.
//
// CASE SURVIVES in a stream alias and is FLATTENED in a template alias. That
// asymmetry is the built-in graph's and it is deliberate: a label value is a
// name a human wrote and `OOMKilled` should read as `OOMKilled`, while a
// template alias is built from prose and is canonicalized so two spellings of
// the same sentence land on one alias.

// sanitize normalizes one alias component: unsafe characters and runs of them
// collapse to a single dash, and leading and trailing dashes are trimmed. Case
// is preserved.
func sanitize(s string) string {
	if s == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(s))
	prevDash := false
	for _, r := range s {
		if isAliasSafe(r) {
			b.WriteRune(r)
			prevDash = false
			continue
		}
		if !prevDash {
			b.WriteByte('-')
			prevDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// isAliasSafe reports whether a rune survives verbatim inside a component.
// `.` and `@` are excluded even though they are printable: they are the
// SEPARATORS between components, so admitting them inside one would make an
// alias ambiguous to split.
func isAliasSafe(r rune) bool {
	switch r {
	case '-', '_', ':':
		return true
	}
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// AliasFor derives a stream's readable identifier from its label set: the two
// lowest-sorting keys with non-empty values, as `<key>=<value>.<key>=<value>`.
//
// THE TWO LOWEST-SORTING KEYS ARE THE WHOLE DERIVATION, which is why the
// collector's label key names are a design decision rather than an incidental
// one. Sorting is byte order over the key names, and a key whose value is empty
// is skipped rather than emitted as `key=`.
//
// It falls back to the low-cardinality labels when the full set is empty, and
// returns the empty string only when both are.
//
// THE BUILT-IN GRAPH'S FOUR PROVIDER-SHAPED ARMS ARE DELIBERATELY ABSENT HERE,
// and their absence changes no emitted value. Those arms are selected by four
// label keys — `reason`, `log_stream`/`log_group`, `resource_type` and `app` —
// and this collector emits none of them: `reason` and its siblings are an
// EVENT-derived vocabulary this module does not read, and `app` would route a
// pod-log stream through a Loki-shaped derivation. So the generic form below is
// the only arm a stream from this collector can reach, and writing the other
// four would be four branches no input selects.
func AliasFor(stream *Stream) string {
	if stream == nil {
		return ""
	}
	labels := stream.Labels
	if len(labels) == 0 {
		labels = stream.LowCardLabels
	}
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k, v := range labels {
		if v == "" {
			continue
		}
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		return ""
	}
	sort.Strings(keys)
	parts := make([]string, 0, aliasKeyCount)
	for i := 0; i < len(keys) && i < aliasKeyCount; i++ {
		parts = append(parts, sanitize(keys[i])+"="+sanitize(labels[keys[i]]))
	}
	return strings.Join(parts, ".")
}

// aliasKeyCount is how many label keys reach a stream alias. Two is enough to
// read and short enough to stay a name rather than a serialization of the label
// set; it is named here because the discrimination requirement on the
// collector's key naming is stated in terms of it.
const aliasKeyCount = 2
