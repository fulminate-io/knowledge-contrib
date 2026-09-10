// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"unicode"
)

// alias_template.go — the TEMPLATE ALIAS: a kebab-cased summary of the pattern
// with a short severity suffix.
//
// IT LOWERCASES AND THE STREAM DERIVER DOES NOT. Both are right: a stream alias
// carries label values an operator searches for verbatim, while a template alias
// is derived from prose whose casing is incidental. Running one case rule
// through both is a divergence no id test can see, because neither alias enters
// an id.

// templateAliasTokens is how many meaningful tokens an alias carries. Five is
// enough to distinguish patterns and short enough to read at a glance.
const templateAliasTokens = 5

// templateAliasFor derives a template's alias from its pattern and severity.
//
// AN EMPTY RESULT IS A REAL OUTCOME, not a failure: a pattern that is nothing
// but stopwords and wildcards has no meaningful token. Callers fall back to the
// raw pattern for the node's readable name, so the node is never nameless.
func templateAliasFor(tmpl *logTemplate) string {
	if tmpl == nil {
		return ""
	}
	tokens := meaningfulTokens(stripReasonPrefix(tmpl.Pattern), templateAliasTokens)
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

// minReasonPrefixLen is the shortest text that can be a reason prefix. One
// letter before a colon is far more likely to be a label than a reason name.
const minReasonPrefixLen = 2

// stripReasonPrefix removes a leading `<Reason>: ` where the reason is a single
// CamelCase word, because that word is already carried by the stream's own
// `reason` label and repeating it in every template alias adds nothing.
//
// The three conditions are what keep it from eating an ordinary sentence: the
// prefix must be at least two characters, start uppercase, and be ALL letters —
// so `error: connection refused` and `GET /x: 404` both pass through whole.
func stripReasonPrefix(pattern string) string {
	idx := strings.Index(pattern, ": ")
	if idx <= 0 || idx >= len(pattern)-2 {
		return pattern
	}
	prefix := pattern[:idx]
	if len(prefix) < minReasonPrefixLen || !unicode.IsUpper(rune(prefix[0])) {
		return pattern
	}
	for _, r := range prefix {
		if !unicode.IsLetter(r) {
			return pattern
		}
	}
	return pattern[idx+2:]
}

// severityShort maps a canonical severity to its alias suffix.
//
// THE EMPTY SEVERITY YIELDS NO SUFFIX and any OTHER value yields its lowercased
// self rather than nothing, so a severity this module did not mint still names
// itself in the alias. Neither arm is reachable from a collected entry — every
// severity a Cloud Logging entry produces goes through mapGCPSeverity, which
// returns one of the six on every arm including its default — so both are unit
// arms on this function rather than shapes a collect can reach.
func severityShort(sev string) string {
	switch sev {
	case severityCritical:
		return "crit"
	case severityError:
		return "err"
	case severityWarn:
		return "warn"
	case severityInfo:
		return "info"
	case severityDebug:
		return "debug"
	case severityTrace:
		return "trace"
	case "":
		return ""
	default:
		return strings.ToLower(sev)
	}
}

// templateStopwords is the connector set dropped from an alias. It is
// deliberately SHORT: every addition risks dropping a word that carries meaning
// in a log line, and "in" is already borderline.
var templateStopwords = map[string]struct{}{
	"a": {}, "an": {}, "the": {},
	"of": {}, "on": {}, "for": {}, "to": {}, "in": {}, "is": {},
	"and": {}, "or": {}, "with": {},
}

// stripWildcards replaces each wildcard with a SPACE rather than deleting it,
// so the tokens on either side stay separate instead of fusing into one.
func stripWildcards(pattern string) string {
	if !strings.Contains(pattern, wildcard) {
		return pattern
	}
	return strings.ReplaceAll(pattern, wildcard, " ")
}

// meaningfulTokens splits a pattern into up to max lowercase alphanumeric
// tokens, dropping stopwords. Splitting on every non-alphanumeric rune is what
// turns `dial tcp 10.0.0.1:443` into separate tokens without a punctuation list.
func meaningfulTokens(pattern string, max int) []string {
	if pattern == "" {
		return nil
	}
	cleaned := stripWildcards(pattern)
	out := make([]string, 0, max)
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
		if len(out) < max {
			out = append(out, tok)
		}
	}
	for _, r := range cleaned {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			cur.WriteRune(r)
			continue
		}
		flush()
		if len(out) >= max {
			return out
		}
	}
	flush()
	return out
}
