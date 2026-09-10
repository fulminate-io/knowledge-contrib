// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"strings"
	"unicode"
)

// alias_template.go — the TEMPLATE alias deriver, which is a SECOND, WHOLLY
// DIFFERENT deriver from the stream one.
//
// It reads the pattern and the severity, not labels; it LOWERCASES every token
// where the stream deriver preserves case; it strips a Kubernetes-style reason
// prefix, wildcards and stopwords; it caps at five tokens; and it appends a
// severity suffix after '@'. Deriving a template's alias with the stream rules
// produces a wrong SymbolName and a wrong alias on every template node, on a
// graph that validates.
//
//	"Node <*> is not ready" / WARN           -> node-not-ready@warn
//	"NodeNotReady: Node is not ready" / ERROR -> node-not-ready@err
//	"<*>" / INFO                              -> "" (the caller falls back to
//	                                             the raw pattern)

// TemplateAliasFor derives a template's readable name from its pattern and
// severity. It returns the empty string when nothing survives the stripping,
// which is why the node builder falls back to the raw pattern for SymbolName.
func TemplateAliasFor(tmpl *Template) string {
	if tmpl == nil {
		return ""
	}
	tokens := meaningfulTokens(stripReasonPrefix(tmpl.Pattern), 5)
	if len(tokens) == 0 {
		return ""
	}
	kebab := kebabCase(tokens)
	suffix := severityShort(tmpl.Severity)
	if suffix == "" {
		return kebab
	}
	return kebab + "@" + suffix
}

// stripReasonPrefix removes a leading "<Reason>: " when the prefix is a single
// all-letter token starting uppercase. Anything else is left alone: the prefix
// heuristic is narrow on purpose, because a wrongly stripped prefix silently
// renames a template.
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

// severityShort maps a canonical severity to its alias suffix. An EMPTY
// severity yields no suffix at all — and therefore no '@' — while a
// non-canonical one yields its own lowercased self, so a provider outside the
// vocabulary still gets a useful name.
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

// templateStopwords is the twelve-word connector set dropped from an alias.
// The list stays short deliberately: a longer one starts dropping meaningful
// terms, and "in" is already borderline.
var templateStopwords = map[string]struct{}{
	"a": {}, "an": {}, "the": {},
	"of": {}, "on": {}, "for": {}, "to": {}, "in": {}, "is": {},
	"and": {}, "or": {}, "with": {},
}

// stripWildcards replaces each wildcard with a space so token splitting still
// separates the words either side of it.
func stripWildcards(pattern string) string {
	if !strings.Contains(pattern, Wildcard) {
		return pattern
	}
	return strings.ReplaceAll(pattern, Wildcard, " ")
}

// meaningfulTokens splits on non-alphanumerics, LOWERCASES, drops stopwords and
// returns at most max tokens. The lowercasing is the cell that distinguishes
// this deriver from the stream one, which preserves case.
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

// kebabCase joins already-lowercased, already-safe tokens with '-'.
func kebabCase(tokens []string) string { return strings.Join(tokens, "-") }
