// SPDX-License-Identifier: Apache-2.0

package main

import (
	"regexp"
	"strings"
)

// preprocess.go — MASKING AND TOKENIZATION, the two stages that decide which
// entries can cluster together at all.
//
// Everything downstream is a hash over this stage's output, so a divergence
// here moves every template id, every chunk id and every alias, silently. The
// rules are the built-in pipeline's, reproduced with the ORDER preserved.

// Wildcard replaces a variable position in a template pattern.
const Wildcard = "<*>"

// urlPlaceholder replaces a matched URL. It is NOT the wildcard: a URL keeps
// its own placeholder so a message that differs only in its URL still clusters
// distinguishably from one that differs in a number.
const urlPlaceholder = "<url>"

// The masking patterns, compiled once at package level.
//
// THE ORDER THEY ARE APPLIED IN IS PART OF THE SPECIFICATION and PreProcess
// applies them in the order below. The numeric rule matches any run of four or
// more digits, so it would eat a bare 10-to-13-digit epoch that the timestamp
// rule is meant to claim; the timestamp rule therefore runs first. Reordering
// these two produces different patterns, different template ids and different
// chunk ids, with no error raised anywhere.
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

// PreProcess replaces the high-cardinality runs in a message with placeholders,
// in the fixed order the package comment above explains: UUID, timestamp, IPv4,
// long hex run, long number, URL.
func PreProcess(msg string) string {
	msg = reUUID.ReplaceAllString(msg, Wildcard)
	msg = reTimestamp.ReplaceAllString(msg, Wildcard)
	msg = reIPv4.ReplaceAllString(msg, Wildcard)
	msg = reHexID.ReplaceAllString(msg, Wildcard)
	msg = reNumeric.ReplaceAllString(msg, Wildcard)
	msg = reURL.ReplaceAllString(msg, urlPlaceholder)
	return msg
}

// Tokenize splits a masked message on whitespace runs.
func Tokenize(msg string) []string { return strings.Fields(msg) }

// tokenCountBucket buckets a token count for the Drain tree's first-level
// branching. Two messages in different buckets can NEVER join one template,
// whatever their similarity, so the boundaries below are behavior and not
// tuning: 3, 8 and 15 are the last count in each bucket.
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

// isWildcard reports whether a token is treated as variable when the Drain tree
// branches on it. A token CONTAINING a digit counts, not only the wildcard
// literal: an unmasked short number is variable in practice, and branching on
// it would split one template per value.
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
