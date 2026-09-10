// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"strings"
	"unicode"
)

// alias.go — the character rules both alias derivers share.
//
// AN ALIAS IS NOT UNIQUE AND THIS COLLECTOR DOES NOT MAKE IT SO. Collisions are
// resolved by the reading layer, which appends a short hash suffix once the
// whole set is known. A collector that de-collided in its own walk would emit
// names the reader then de-collides again.

// firstNonEmpty returns the first NON-EMPTY VALUE at the supplied keys. It
// tests the value, not the key: a key present with an empty value falls through
// to the next link of the chain, which is the cell an implementation that
// checks presence gets wrong.
func firstNonEmpty(labels map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := labels[k]; v != "" {
			return v
		}
	}
	return ""
}

// sanitize collapses each run of unsafe characters to a single '-' and trims
// leading and trailing '-'. CASE IS PRESERVED, so a Kubernetes reason of
// OOMKilled survives intact; the template deriver lowercases instead, and that
// disagreement is deliberate.
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

// isAliasSafe reports whether a rune survives verbatim inside an alias
// COMPONENT. '.' and '@' are reserved as separators BETWEEN components, so they
// are unsafe here even though they appear in the finished alias.
func isAliasSafe(r rune) bool {
	switch r {
	case '-', '_', ':':
		return true
	}
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

// ShortHash is the first 8 hex characters of an id — the suffix the reading
// layer appends on a collision. An input shorter than that is returned whole.
func ShortHash(id string) string {
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}
