// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"regexp"
	"strings"
)

// preprocess.go — what a message looks like before Drain sees it.
//
// THE POINT IS TEMPLATE COUNT. Drain clusters on token identity, so a message
// carrying a request id, a timestamp or an address produces a fresh cluster per
// line and the template set degenerates into a copy of the log. Replacing the
// known high-cardinality shapes with the wildcard FIRST is what makes the
// clustering converge, and it is why the same text preprocessed twice always
// lands in the same cluster.

// Wildcard is the placeholder token standing for a variable part.
const Wildcard = "<*>"

// The high-cardinality shapes replaced before clustering. Order matters at
// exactly one place and it is applied in [PreProcess]: UUIDs run before the hex
// rule, because a UUID's segments would otherwise be eaten piecemeal.
var (
	reTimestamp = regexp.MustCompile(
		`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:?\d{2})?` +
			`|\d{2}:\d{2}:\d{2}(?:\.\d+)?` +
			`|\d{10,13}`)
	reUUID    = regexp.MustCompile(`[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}`)
	reIPv4    = regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`)
	reHexID   = regexp.MustCompile(`\b[0-9a-fA-F]{16,}\b`)
	reNumeric = regexp.MustCompile(`\b\d{4,}\b`)
	reURL     = regexp.MustCompile(`https?://[^\s'")\]]{20,}`)
)

// PreProcess replaces the high-cardinality shapes in a message with wildcards.
// A long URL becomes the literal "<url>" rather than the wildcard, so a
// template whose only variable part is an endpoint still reads as one.
func PreProcess(msg string) string {
	msg = reUUID.ReplaceAllString(msg, Wildcard)
	msg = reTimestamp.ReplaceAllString(msg, Wildcard)
	msg = reIPv4.ReplaceAllString(msg, Wildcard)
	msg = reHexID.ReplaceAllString(msg, Wildcard)
	msg = reNumeric.ReplaceAllString(msg, Wildcard)
	msg = reURL.ReplaceAllString(msg, "<url>")
	return msg
}

// Tokenize splits a preprocessed message on whitespace.
func Tokenize(msg string) []string { return strings.Fields(msg) }

// tokenCountBucket maps a token count to the parse tree's first-level branch.
// Bucketing rather than branching on the exact count is what stops a
// one-token-longer variant of the same message from landing in a tree of its
// own.
func tokenCountBucket(n int) string {
	switch {
	case n <= 3:
		return "short"
	case n <= 8:
		return "medium"
	case n <= 15:
		return "long"
	default:
		return "vlong"
	}
}

// isWildcard reports whether a token should be treated as variable when the
// parse tree branches on it. A token containing a digit counts: an id, a count
// or a port in the first few positions would otherwise fan the tree out one
// branch per value.
func isWildcard(token string) bool {
	if token == Wildcard {
		return true
	}
	return containsDigit(token)
}

// containsDigit reports whether s holds an ASCII digit.
func containsDigit(s string) bool {
	for _, r := range s {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}
