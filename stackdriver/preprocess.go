// SPDX-License-Identifier: Apache-2.0

package main

import (
	"regexp"
	"strings"
)

// preprocess.go — the HIGH-CARDINALITY MASKING that runs before clustering, and
// the tokenizer the clusterer works in.
//
// Without this pass every request id, every timestamp and every address is a
// distinct token, so two renderings of the same log line never cluster and the
// template set grows linearly with the entry count. Masking them first is what
// makes a template a template.

// wildcard is the placeholder token for a variable position. It appears in the
// template pattern, which is hashed into the template id, so its spelling is
// part of the emitted graph's identity rather than a display choice.
const wildcard = "<*>"

// urlPlaceholder replaces a long URL. It is distinct from wildcard because a
// URL is a variable the reader wants named: a pattern reading "GET <url>
// failed" says more than one reading "GET <*> failed".
const urlPlaceholder = "<url>"

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

// preProcess masks the high-cardinality token classes.
//
// THE ORDER IS PART OF THE RULE. UUIDs go first because their hex runs would
// otherwise be eaten by the hex-id pattern, and timestamps before the numeric
// pattern for the same reason; URLs go last because the earlier passes may have
// already masked what is inside one, and masking the whole URL after that still
// yields a single token.
func preProcess(msg string) string {
	msg = reUUID.ReplaceAllString(msg, wildcard)
	msg = reTimestamp.ReplaceAllString(msg, wildcard)
	msg = reIPv4.ReplaceAllString(msg, wildcard)
	msg = reHexID.ReplaceAllString(msg, wildcard)
	msg = reNumeric.ReplaceAllString(msg, wildcard)
	msg = reURL.ReplaceAllString(msg, urlPlaceholder)
	return msg
}

// tokenize splits a masked message on whitespace.
func tokenize(msg string) []string { return strings.Fields(msg) }

// tokenCountBucket maps a token count to the parse tree's first-level branch.
// Bucketing rather than branching on the exact count is what lets two renderings
// of one message that differ by a word still meet in the same subtree.
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

// isWildcardToken reports whether a token should be treated as variable when
// walking the parse tree. A token carrying a digit is treated as variable even
// when the masking pass left it alone, because a short number is the commonest
// unmasked variable there is.
func isWildcardToken(token string) bool {
	if token == wildcard {
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
