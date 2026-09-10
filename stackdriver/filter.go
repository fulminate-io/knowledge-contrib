// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/logging"
)

// filter.go — the ADVANCED LOGS FILTER this collector sends, its escaping, and
// the two severity maps.
//
// THE FILTER STRING IS THE ONE INJECTION SURFACE THIS COLLECTOR HAS. Every
// value in it arrives from a tool call, which is to say from a language model
// or from whatever drove one, so each is sanitized to a safe character set and
// then escaped for the quoted-string context it lands in. RawQuery is the one
// deliberate exception and it is documented as such on the params type: an
// operator asking for a native filter expression is asking for exactly that.

// gcpFieldMapping translates the canonical field names a caller may use in
// FieldFilters to the paths Cloud Logging indexes them under. A name absent
// from this map passes through unchanged, so a caller can filter on a
// provider-specific field without this module learning it first.
var gcpFieldMapping = map[string]string{
	fieldService:   "resource.labels.service_name",
	fieldHost:      "resource.labels.instance_id",
	fieldNamespace: "resource.labels.namespace_name",
	fieldPod:       "resource.labels.pod_name",
	fieldContainer: "resource.labels.container_name",
	fieldLevel:     "severity",
}

// The canonical field names FieldFilters accepts.
const (
	fieldService   = "service"
	fieldHost      = "host"
	fieldNamespace = "namespace"
	fieldPod       = "pod"
	fieldContainer = "container"
	fieldLevel     = "level"
)

// logQuery is the read this collector performs, assembled from the tool's
// params. It is the module's own shape rather than a wire type.
type logQuery struct {
	Source       string
	StartTime    time.Time
	EndTime      time.Time
	TextFilter   string
	FieldFilters map[string]string
	SeverityMin  string
	MaxEntries   int
	RawQuery     string
}

// buildFilter assembles the Advanced Logs Filter string. Clauses are joined by
// newlines, which Cloud Logging reads as a logical AND, and the clause ORDER is
// fixed rather than map-derived so the same query produces the same filter on
// every run and in every process.
func buildFilter(projectID string, q logQuery) string {
	var parts []string

	if !q.StartTime.IsZero() {
		parts = append(parts, fmt.Sprintf(`timestamp >= "%s"`, q.StartTime.UTC().Format(time.RFC3339)))
	}
	if !q.EndTime.IsZero() {
		parts = append(parts, fmt.Sprintf(`timestamp <= "%s"`, q.EndTime.UTC().Format(time.RFC3339)))
	}
	if q.SeverityMin != "" {
		if gcpSev := mapSeverityToGCP(q.SeverityMin); gcpSev != "" {
			parts = append(parts, "severity >= "+gcpSev)
		}
	}
	if q.Source != "" {
		parts = append(parts, buildLogNameClause(projectID, q.Source))
	}
	if q.TextFilter != "" {
		parts = append(parts, escapeTextFilter(q.TextFilter))
	}
	if len(q.FieldFilters) > 0 {
		mapped := mapFieldFilters(q.FieldFilters)
		fields := make([]string, 0, len(mapped))
		for field := range mapped {
			fields = append(fields, field)
		}
		sort.Strings(fields)
		for _, field := range fields {
			parts = append(parts, fmt.Sprintf(`%s="%s"`, field, escapeValue(mapped[field])))
		}
	}
	if q.RawQuery != "" {
		parts = append(parts, q.RawQuery)
	}

	return strings.Join(parts, "\n")
}

// buildLogNameClause accepts a short log id ("stderr") or a fully qualified one
// ("projects/other/logs/stdout"), expanding the short form against the
// collect's own project so an operator need not type the prefix.
func buildLogNameClause(projectID, source string) string {
	safe := escapeValue(sanitizeSourceName(source))
	if strings.HasPrefix(safe, "projects/") {
		return fmt.Sprintf(`logName="%s"`, safe)
	}
	return fmt.Sprintf(`logName="projects/%s/logs/%s"`, projectID, safe)
}

// escapeTextFilter wraps a text filter in quotes, escaping what is inside.
//
// A FILTER THE CALLER ALREADY QUOTED IS NOT RE-QUOTED, so a hand-written
// exact-match expression does not become nonsense — but its INTERIOR is escaped
// on exactly the same terms as a bare one. Those are two different properties
// and only the first was ever the point of the pass-through: returning the value
// verbatim let a quoted text filter carry a raw line break, which is the clause
// separator, so it could close its own clause and open another. params.filter is
// this collector's ONE deliberate verbatim input; text_filter is documented as a
// substring match and must not be a second one by accident.
//
// A lone quote is not a quoted filter: it has no interior, so it takes the
// ordinary arm rather than being sliced to nothing.
func escapeTextFilter(text string) string {
	if len(text) >= 2 && strings.HasPrefix(text, `"`) && strings.HasSuffix(text, `"`) {
		return `"` + escapeValue(text[1:len(text)-1]) + `"`
	}
	return `"` + escapeValue(text) + `"`
}

// escapeValue escapes a value for a double-quoted filter string.
//
// THE BACKSLASH PASS RUNS FIRST: escaping the quotes first would then double the
// backslashes this pass introduces, and the value would decode differently.
//
// THE LINE BREAKS ARE ESCAPED TOO AND THAT IS NOT DECORATION. Clauses in this
// filter language are separated by NEWLINES, so a value carrying a raw newline
// is a value that can end its own clause and begin another one — quoting it does
// not help if the language does not admit a raw newline inside a quoted string,
// and whether it does is not something to bet an injection on. Escaping them
// means a value is one clause whatever it contains. Escaping is the primary
// defense here; sanitizeQueryValue is the second layer, and neither is applied
// to params.filter, which is a native expression by request.
func escapeValue(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\r", `\r`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return s
}

// mapFieldFilters translates canonical field names to their Cloud Logging
// paths, sanitizing both halves. A name that sanitizes to nothing is dropped:
// it named no field, and passing it through would emit `="value"`.
func mapFieldFilters(filters map[string]string) map[string]string {
	if len(filters) == 0 {
		return nil
	}
	result := make(map[string]string, len(filters))
	for k, v := range filters {
		k = sanitizeFieldName(k)
		v = sanitizeQueryValue(v)
		if k == "" {
			continue
		}
		if native, ok := gcpFieldMapping[k]; ok {
			result[native] = v
		} else {
			result[k] = v
		}
	}
	return result
}

// sanitizeFieldName restricts a field name to the characters a field path can
// legitimately hold, so a name cannot carry a filter operator.
func sanitizeFieldName(name string) string {
	var sb strings.Builder
	sb.Grow(len(name))
	for _, r := range name {
		if isAlphanumeric(r) || r == '.' || r == '_' || r == '-' {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// sanitizeSourceName restricts a log name to the characters a log id can hold,
// which includes the slash and the wildcard a qualified name needs.
func sanitizeSourceName(name string) string {
	var sb strings.Builder
	sb.Grow(len(name))
	for _, r := range name {
		if isAlphanumeric(r) || r == '.' || r == '_' || r == '-' || r == '/' || r == '*' {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// sanitizeQueryValue strips the characters that act as operators or control
// flow in a filter expression. Escaping is the primary defense; this is the
// second layer, and it is what stops a value from carrying a clause at all.
func sanitizeQueryValue(value string) string {
	var sb strings.Builder
	sb.Grow(len(value))
	for _, r := range value {
		switch {
		case isAlphanumeric(r):
			sb.WriteRune(r)
		case r == ' ' || r == '.' || r == '-' || r == '_' || r == ':' ||
			r == '/' || r == '@' || r == '+' || r == '=' || r == ',':
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// isAlphanumeric reports whether r is an ASCII letter or digit. It is ASCII on
// purpose: a filter field path is ASCII, and admitting the wider Unicode letter
// class would admit homoglyphs of the operators the sanitizers strip.
func isAlphanumeric(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// mapSeverityToGCP maps a canonical severity to the Cloud Logging severity name
// a filter clause uses. An unmapped value returns the empty string and
// buildFilter then emits NO severity clause, which is the honest reading of "no
// minimum severity I can express" — a clause naming an unknown level would be
// rejected by the API for the whole query.
func mapSeverityToGCP(severity string) string {
	switch severity {
	case severityTrace, severityDebug:
		return "DEBUG"
	case severityInfo:
		return "INFO"
	case severityWarn:
		return "WARNING"
	case severityError:
		return "ERROR"
	case severityCritical:
		return "CRITICAL"
	default:
		return ""
	}
}

// mapGCPSeverity is the inverse: it maps the SDK's numeric severity back to a
// canonical name.
//
// THE COMPARISONS ARE `>=` BECAUSE THE NUMERIC SPACE IS SPARSE. Cloud Logging
// admits values BETWEEN the named levels, and the arms are ordered from most to
// least severe, so an in-between value takes the highest named level at or below
// it: 450, which sits between Warning and Error, reads as WARN. That is a FLOOR
// rather than a round-up, and it is the conservative direction — an unnamed
// level is not promoted into a bucket an operator filters errors by.
//
// (The knowledge client's own adapter carries the identical switch under a
// comment claiming it rounds up. The behaviour here is deliberately identical,
// because the value reached is what a re-collect has to reproduce; only the
// description is corrected.)
//
// EVERY ARM RETURNS A CANONICAL NAME, INCLUDING THE DEFAULT, which is what makes
// the empty-severity alias arm unreachable from a collected entry.
func mapGCPSeverity(s logging.Severity) string {
	switch {
	case s >= logging.Emergency, s >= logging.Alert, s >= logging.Critical:
		return severityCritical
	case s >= logging.Error:
		return severityError
	case s >= logging.Warning:
		return severityWarn
	case s >= logging.Notice, s >= logging.Info:
		return severityInfo
	case s >= logging.Debug:
		return severityDebug
	default:
		return severityInfo
	}
}
