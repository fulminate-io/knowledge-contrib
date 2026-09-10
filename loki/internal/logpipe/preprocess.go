// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"regexp"
	"strings"
)

// preprocess.go — the token normalisation that runs BEFORE clustering.
//
// Every rule here decides the pattern text, and the pattern text is hashed into
// the template id, so a difference of one regex is a different template set and
// a different chunk set under it.

// Wildcard is the placeholder token standing for a variable part of a pattern.
const Wildcard = "<*>"

// The high-cardinality token classes replaced before clustering. Order matters:
// the UUID pass runs before the numeric passes so a UUID is one wildcard rather
// than several.
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

// PreProcess replaces the high-cardinality token classes with wildcards. A URL
// becomes the literal <url> rather than a wildcard, so a pattern still says
// that a URL was there.
func PreProcess(msg string) string {
	msg = reUUID.ReplaceAllString(msg, Wildcard)
	msg = reTimestamp.ReplaceAllString(msg, Wildcard)
	msg = reIPv4.ReplaceAllString(msg, Wildcard)
	msg = reHexID.ReplaceAllString(msg, Wildcard)
	msg = reNumeric.ReplaceAllString(msg, Wildcard)
	msg = reURL.ReplaceAllString(msg, "<url>")
	return msg
}

// Tokenize splits a preprocessed message on whitespace runs.
func Tokenize(msg string) []string { return strings.Fields(msg) }

// tokenCountBucket is the parse tree's first-level branch key. Two messages in
// different buckets never compare against each other.
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

// isWildcard reports whether a token is treated as variable when walking the
// parse tree. A token carrying any digit is treated as variable even when
// PreProcess left it alone, because a short number is still an identifier.
func isWildcard(token string) bool {
	if token == Wildcard {
		return true
	}
	return containsDigit(token)
}

func containsDigit(s string) bool {
	for _, r := range s {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}
