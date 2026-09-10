// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"unicode"
)

// alias.go — the shared half of ALIAS DERIVATION: readable identifiers so a
// reader can name a stream or a template without quoting a hash.
//
// AN ALIAS IS NOT UNIQUE AND THIS MODULE DOES NOT MAKE IT SO. Two streams can
// derive the same alias; resolving that is the reading layer's job, which
// appends a short hash suffix when it sees a collision and can do so because it
// holds the whole set. A collector that pre-suffixed would emit a name the
// reading layer never produces, and the suffix separators differ between the two
// object kinds, so the mistake would not even be uniform.
//
// CASE IS PRESERVED IN A STREAM ALIAS AND LOWERCASED IN A TEMPLATE ALIAS. That
// is deliberate on both sides and is the one rule most easily run through both:
// a Kubernetes reason reads as `OOMKilled` because that is the string an
// operator searches for, while a template alias is derived from prose and is
// canonicalized.

// firstNonEmpty returns the first non-empty value among the given keys, in the
// order given. The ORDER is the rule, not an implementation detail: it is what
// decides which label names a stream when several are present.
func firstNonEmpty(labels map[string]string, keys ...string) string {
	for _, k := range keys {
		if v := labels[k]; v != "" {
			return v
		}
	}
	return ""
}

// sanitize normalizes one alias component: safe characters survive, every run
// of anything else collapses to a single dash, and leading and trailing dashes
// are trimmed. Empty input yields empty output rather than a bare dash.
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
// `.` AND `@` ARE DELIBERATELY UNSAFE at the component level even though both
// are printable: they are the SEPARATORS the derivers join components with, so
// admitting them inside a component would make the alias ambiguous about where
// one component ends.
func isAliasSafe(r rune) bool {
	switch r {
	case '-', '_', ':':
		return true
	}
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}
