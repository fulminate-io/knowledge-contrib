// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"unicode"
)

// alias.go — THE SHARED ALIAS HELPERS. An alias is the readable identifier a
// node carries as its SymbolName, which is what BM25 matches, so a divergence
// here is not cosmetic: it renames every node of that kind.
//
// ALIASES ARE NOT UNIQUE AND THIS COLLECTOR DOES NOT MAKE THEM UNIQUE. The
// built-in pipeline resolves collisions on the READ side, once the whole stream
// set is known, by appending a short hash. A collector that de-collided during
// its own walk would produce aliases the reader then de-collides again, so
// collision handling is deliberately absent here.

// firstNonEmpty returns the first non-empty VALUE among the given keys. It
// tests the value rather than the key's presence, so a key present with an
// empty value falls through to the next candidate.
func firstNonEmpty(labels map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := labels[k]; v != "" {
			return v
		}
	}
	return ""
}

// sanitize normalizes one alias COMPONENT.
//
// Each run of unsafe characters collapses to a single "-", leading and trailing
// dashes are trimmed, and CASE IS PRESERVED, so a mixed-case stream name
// survives intact.
//
// It is applied per component and never to a joined alias: the separators
// between components are themselves unsafe characters, so sanitizing the joined
// string would collapse them.
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

// isAliasSafe reports whether a rune survives sanitize verbatim.
//
// THE SAFE SET IS NOT THE OBVIOUS ONE and this is the half a reimplementation
// gets wrong: underscore and colon are SAFE and survive inside a component,
// while "." and "@" are reserved separators and are NOT safe, because they
// appear only BETWEEN components. The ordinary AWS shape reaches this
// immediately — a log group /aws/lambda/my_function derives the service
// my_function, which the obvious `[^A-Za-z0-9]` rule would render my-function:
// a different alias and a different SymbolName on every underscore-bearing log
// group, with no error raised anywhere.
func isAliasSafe(r rune) bool {
	switch r {
	case '-', '_', ':':
		return true
	}
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}
