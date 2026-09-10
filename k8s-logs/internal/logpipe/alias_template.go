// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"strings"
	"unicode"
)

// alias_template.go — a template's readable identifier, derived from its
// pattern and its severity through six rules in a fixed order.
//
// IT IS RECOMPUTED, NOT DERIVED ONCE. A template's pattern broadens whenever
// its cluster absorbs an entry, so an alias computed at creation and left alone
// describes a pattern the template no longer has. The recompute lives in the
// clustering path and in the consolidators, and a test asserts the alias AFTER
// a merge for exactly this reason.

// templateAliasTokenLimit is how many meaningful tokens reach the alias.
const templateAliasTokenLimit = 5

// TemplateAliasFor derives a template's alias as `<kebab>@<severity>`, or the
// bare kebab form when the severity is empty, or the empty string when no token
// survives the stripping.
//
// The rules, in the order they apply: a leading CamelCase `Word: ` prefix is
// dropped; wildcards become spaces; the remainder is lowercased, split on
// non-alphanumerics, stripped of a small stopword set and cut to the first
// [templateAliasTokenLimit] tokens; those are joined with dashes; and the
// severity is appended in its short form.
//
// THE PREFIX STRIP FIRES ON ORDINARY LOG TEXT and that surprises people. It was
// written for a Kubernetes event's `<Reason>: <Note>` composition, but it is
// unconditional, so a line reading `ERROR: connection refused to <*>` derives
// `connection-refused@err` with the level word gone. That is correct behavior
// to reproduce rather than a bug to fix here: the alias is a name, the level is
// already carried in the suffix, and diverging from the shared derivation would
// mean this graph's names do not match the graph it is at parity with.
func TemplateAliasFor(tmpl *Template) string {
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

// stripReasonPrefix drops a leading `Word: ` when Word is letters-only and
// starts uppercase. Anything else — a lowercase prefix like `http: `, a prefix
// carrying a digit or a colon at either end of the string — is left alone.
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

// severityShort maps a canonical level to its alias suffix. An unrecognized
// level is lowercased rather than dropped, so a suffix stays useful even when
// the level came from somewhere this package does not know.
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

// templateStopwords is the connector set dropped from an alias. It is
// deliberately SHORT: every addition risks eating a word that carries the
// meaning of a log line, and `in` is already close to that edge.
var templateStopwords = map[string]struct{}{
	"a": {}, "an": {}, "the": {},
	"of": {}, "on": {}, "for": {}, "to": {}, "in": {}, "is": {},
	"and": {}, "or": {}, "with": {},
}

// stripWildcards replaces each wildcard with a space so token splitting still
// sees a boundary where the variable part was.
func stripWildcards(pattern string) string {
	if !strings.Contains(pattern, Wildcard) {
		return pattern
	}
	return strings.ReplaceAll(pattern, Wildcard, " ")
}

// meaningfulTokens returns the first limit alias tokens of a pattern:
// lowercased, split on non-alphanumerics, stopwords dropped.
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
