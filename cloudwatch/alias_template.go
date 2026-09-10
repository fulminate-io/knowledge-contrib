// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"unicode"
)

// alias_template.go — THE TEMPLATE ALIAS DERIVER, one of TWO alias derivers in
// this package that share no code and disagree about case.
//
// This one takes a PATTERN and an AGGREGATE SEVERITY through a seven-step token
// pipeline and LOWERCASES every token. The stream deriver (alias_stream.go)
// takes a LABEL SET through a provider dispatch and PRESERVES case. Writing one
// rule and applying it to both is the single easiest way to diverge here, and
// it renames every node of one of the two kinds.

// templateAliasTokenLimit is how many meaningful tokens an alias keeps. The
// scan stops once it holds this many, so a later token can never displace an
// earlier one.
const templateAliasTokenLimit = 5

// TemplateAliasFor derives a template's readable alias from its pattern and its
// AGGREGATE severity — the maximum over the cluster, not any one entry's.
//
// The pipeline, in order: strip a leading "<Reason>: " prefix; replace each
// wildcard with a space so it is a token BOUNDARY and never a token; split on
// non-alphanumeric runes and lowercase; drop the twelve stopwords; keep the
// first five survivors; join them with "-"; append "@" and the short severity.
//
// It returns "" when no meaningful token survives. The caller decides what to
// do with that — see templateNode, which falls back to the pattern for the
// SymbolName and omits the alias metadata key entirely rather than writing an
// empty one.
func TemplateAliasFor(tmpl *LogTemplate) string {
	if tmpl == nil {
		return ""
	}
	tokens := meaningfulTokens(stripReasonPrefix(tmpl.Pattern), templateAliasTokenLimit)
	if len(tokens) == 0 {
		return ""
	}
	kebab := strings.Join(tokens, "-")
	suffix := severityShort(tmpl.Severity)
	if suffix == "" {
		return kebab
	}
	return kebab + "@" + suffix
}

// stripReasonPrefix removes a leading "<Reason>: " when the text before the
// FIRST ": " is at least two characters, starts with an uppercase rune and is
// ALL LETTERS.
//
// THE THREE CONDITIONS ARE THE WHOLE RULE and each one is reachable: a
// single-character prefix is kept, a lowercase-initial prefix is kept, and a
// prefix containing anything but letters is kept — so "GET /x: 200 ok" keeps
// its prefix where "FailedMount: ..." loses it. A plain split on ": " passes
// every ordinary case and fails all three.
func stripReasonPrefix(pattern string) string {
	idx := strings.Index(pattern, ": ")
	if idx <= 0 || idx >= len(pattern)-2 {
		return pattern
	}
	prefix := pattern[:idx]
	if len(prefix) < 2 || !unicode.IsUpper(rune(prefix[0])) {
		return pattern
	}
	for _, r := range prefix {
		if !unicode.IsLetter(r) {
			return pattern
		}
	}
	return pattern[idx+2:]
}

// severityShort maps a severity to the alias suffix that follows "@".
//
// Three arms, all reachable: a canonical severity yields its short form; the
// EMPTY severity yields "", which means the alias carries no "@" at all; and
// anything else yields a LOWERCASED COPY, so a non-standard provider severity
// still produces a suffix rather than silently losing one.
func severityShort(sev string) string {
	switch sev {
	case SeverityCritical:
		return "crit"
	case SeverityError:
		return "err"
	case SeverityWarn:
		return "warn"
	case SeverityInfo:
		return "info"
	case SeverityDebug:
		return "debug"
	case SeverityTrace:
		return "trace"
	case "":
		return ""
	default:
		return strings.ToLower(sev)
	}
}

// templateStopwords is the twelve connectors an alias drops. The list is
// deliberately short: every addition risks dropping a meaningful term, and
// "in" is already on it beside "is".
var templateStopwords = map[string]struct{}{
	"a": {}, "an": {}, "the": {},
	"of": {}, "on": {}, "for": {}, "to": {}, "in": {}, "is": {},
	"and": {}, "or": {}, "with": {},
}

// stripWildcards replaces each wildcard with a SPACE rather than deleting it,
// which is what makes a wildcard a token boundary: the words on either side
// stay two tokens instead of joining into one.
func stripWildcards(pattern string) string {
	if !strings.Contains(pattern, Wildcard) {
		return pattern
	}
	return strings.ReplaceAll(pattern, Wildcard, " ")
}

// meaningfulTokens splits a pattern into at most limit lowercased, non-stopword
// tokens. Runes that are neither letters nor digits are separators, so
// punctuation splits and digits stay inside a token.
func meaningfulTokens(pattern string, limit int) []string {
	if pattern == "" {
		return nil
	}
	cleaned := stripWildcards(pattern)
	out := make([]string, 0, limit)
	var cur strings.Builder
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		tok := strings.ToLower(cur.String())
		cur.Reset()
		if _, stop := templateStopwords[tok]; stop {
			return
		}
		if len(out) < limit {
			out = append(out, tok)
		}
	}
	for _, r := range cleaned {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cur.WriteRune(r)
			continue
		}
		flush()
		if len(out) >= limit {
			return out
		}
	}
	flush()
	return out
}
