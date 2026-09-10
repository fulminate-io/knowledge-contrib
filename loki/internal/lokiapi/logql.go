// SPDX-License-Identifier: Apache-2.0

package lokiapi

import (
	"fmt"
	"sort"
	"strings"
)

// logql.go — the LogQL a collect sends.
//
// A query is a STREAM SELECTOR plus optional line filters. The selector either
// comes from the operator verbatim or is built from the structured filters, and
// Loki requires at least one matcher inside it, so a query with nothing to
// select falls back to a selector that matches every namespace rather than
// being sent as `{}` and refused.

// defaultSelector is what a query with no selector and no structured filter
// sends. It is not a wildcard over everything Loki holds — it selects streams
// that HAVE a namespace label — but it is what a Kubernetes-shaped Loki means
// by "everything".
const defaultSelector = `{namespace=~".+"}`

// Query is one collect's read: which streams, over what window, filtered how.
type Query struct {
	// Selector is a LogQL stream selector supplied verbatim, including its
	// braces. When it is empty the selector is built from Source and
	// FieldFilters instead.
	Selector string
	// Source selects a namespace, which is the Kubernetes-shaped convention a
	// Loki instance usually follows.
	Source string
	// FieldFilters are canonical field names mapped to Loki's own label names.
	FieldFilters map[string]string
	// TextFilter is a case-insensitive substring the message must contain. It
	// is sent to Loki as a line filter AND applied on this side, because Loki's
	// line filter is case-sensitive.
	TextFilter string
	// SeverityMin drops entries below a severity. It is applied on this side
	// only: severity is derived from the line body, which Loki cannot see.
	SeverityMin string
	// RawQuery is appended to the built query verbatim, for the LogQL stages a
	// structured shape cannot express.
	RawQuery string
}

// lokiFieldNames maps the canonical field names a caller writes to the label
// names a Loki instance carries. An unmapped name passes through, because a
// Loki's labels are whoever configured the shipper's and a canonical set cannot
// enumerate them.
var lokiFieldNames = map[string]string{
	"service":   "service_name",
	"host":      "node",
	"namespace": "namespace",
	"pod":       "pod",
	"container": "container",
	"level":     "level",
}

// BuildLogQL renders the query.
//
// THE `level` FILTER IS DELIBERATELY EXCLUDED FROM THE SELECTOR. Severity is
// derived from the line body on this side, so selecting on a `level` LABEL
// would drop every entry whose level is in its body and not in its labels —
// which is most of them.
//
// The matchers are SORTED. They come from a Go map, and an unsorted join would
// make two collects over identical input send different query strings.
func BuildLogQL(q Query) string {
	selector := strings.TrimSpace(q.Selector)
	if selector == "" {
		selector = buildSelector(q)
	}

	var b strings.Builder
	b.WriteString(selector)
	if q.TextFilter != "" {
		fmt.Fprintf(&b, ` |= %q`, q.TextFilter)
	}
	if q.RawQuery != "" {
		b.WriteString(" ")
		b.WriteString(q.RawQuery)
	}
	return b.String()
}

// buildSelector assembles a selector from the structured filters, falling back
// to defaultSelector when they name nothing.
func buildSelector(q Query) string {
	var matchers []string
	if q.Source != "" {
		matchers = append(matchers, fmt.Sprintf(`namespace=%q`, sanitizeSourceName(q.Source)))
	}
	for field, value := range q.FieldFilters {
		label := sanitizeFieldName(field)
		if label == "" {
			continue
		}
		if native, ok := lokiFieldNames[label]; ok {
			label = native
		}
		if label == "level" {
			continue
		}
		matchers = append(matchers, fmt.Sprintf(`%s=%q`, label, sanitizeQueryValue(value)))
	}
	if len(matchers) == 0 {
		return defaultSelector
	}
	sort.Strings(matchers)
	return "{" + strings.Join(matchers, ", ") + "}"
}

// sanitizeFieldName restricts a label name to characters that cannot introduce
// a LogQL operator. The filters reach this collector from a tool call, so they
// are untrusted input in exactly the way a query parameter is.
func sanitizeFieldName(name string) string {
	return keepRunes(name, func(r rune) bool {
		return isAlphanumeric(r) || r == '.' || r == '_' || r == '-'
	})
}

// sanitizeSourceName additionally admits the path and wildcard characters a
// namespace or index name legitimately carries.
func sanitizeSourceName(name string) string {
	return keepRunes(name, func(r rune) bool {
		return isAlphanumeric(r) || r == '.' || r == '_' || r == '-' || r == '/' || r == '*'
	})
}

// sanitizeQueryValue strips the characters that act as operators or control
// flow in a log query language. It is defense in depth beside the %q quoting
// the matcher is rendered with, not a replacement for it.
func sanitizeQueryValue(value string) string {
	return keepRunes(value, func(r rune) bool {
		switch r {
		case ' ', '.', '-', '_', ':', '/', '@', '+', '=', ',':
			return true
		}
		return isAlphanumeric(r)
	})
}

func keepRunes(s string, keep func(rune) bool) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if keep(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func isAlphanumeric(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}
