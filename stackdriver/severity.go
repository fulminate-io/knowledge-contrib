// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// severity.go — the CANONICAL SEVERITY VOCABULARY, the embedded-level detector
// the GKE reclassification runs on, and the payload message extractor.
//
// The six names and their order are the vocabulary the emitted graph is read
// with: a template's `severity` metadata value, the `@suffix` on its alias and
// the error-template filter the correlation pass applies all key on them, so a
// seventh name or a different spelling is a graph nobody can query with the
// same predicates.

// The six canonical severities, ordered least to most severe.
const (
	severityTrace    = "TRACE"
	severityDebug    = "DEBUG"
	severityInfo     = "INFO"
	severityWarn     = "WARN"
	severityError    = "ERROR"
	severityCritical = "CRITICAL"
)

// severityOrder is the comparison order. A name absent from this map ranks
// ZERO, which is below TRACE — so an unmapped severity never satisfies a
// minimum-severity predicate. That is deliberate: an unknown level is not
// evidence of an error.
var severityOrder = map[string]int{
	severityTrace:    0,
	severityDebug:    1,
	severityInfo:     2,
	severityWarn:     3,
	severityError:    4,
	severityCritical: 5,
}

// parseSeverityStrict normalizes a level word to a canonical severity, reporting
// whether the word was recognized at all.
//
// THE OK RESULT IS WHAT KEEPS A CONFIGURED VALUE FROM BEING COERCED. The same
// vocabulary is read from two places with opposite requirements: a scrap of log
// text, where an unrecognized word is ordinary and must not fail a collect, and
// a tool PARAMETER, where an unrecognized word is a caller mistake that must be
// refused rather than quietly becoming INFO. One function with two callers is
// how those stay one vocabulary.
func parseSeverityStrict(s string) (string, bool) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case severityTrace:
		return severityTrace, true
	case severityDebug, "DBG":
		return severityDebug, true
	case severityInfo, "INFORMATION", "NOTICE":
		return severityInfo, true
	case severityWarn, "WARNING", "WARNI":
		return severityWarn, true
	case severityError, "ERR", "SEVERE", "FATAL":
		return severityError, true
	case severityCritical, "CRIT", "ALERT", "EMERGENCY", "EMERG", "PANIC":
		return severityCritical, true
	default:
		return severityInfo, false
	}
}

// parseSeverity normalizes a level word found in a MESSAGE BODY. An
// unrecognized word reads as INFO rather than as an error, because the input is
// a scrap of log text and refusing it would fail a collect over one odd line.
// A configured value takes parseSeverityStrict instead.
func parseSeverity(s string) string {
	sev, _ := parseSeverityStrict(s)
	return sev
}

// severityAtLeast reports whether severity is at or above minSeverity.
func severityAtLeast(severity, minSeverity string) bool {
	return severityOrder[severity] >= severityOrder[minSeverity]
}

// severityIndex is the numeric rank of a canonical severity.
func severityIndex(severity string) int { return severityOrder[severity] }

// reEmbeddedLevel matches the level indicators that appear INSIDE a message
// body. It exists because GKE's container runtime tags everything a container
// wrote to stderr as ERROR whatever the application meant, so the wrapper's
// severity is less trustworthy than the line itself.
var reEmbeddedLevel = regexp.MustCompile(
	`(?i)` +
		`(?:` +
		`"?level"?\s*[:=]\s*"?(\w+)"?` + // level=info, "level":"info", level: warn
		`|` +
		`\t(trace|debug|info|warn(?:ing)?|error|fatal|panic)\t` + // zerolog tab-delimited
		`|` +
		`\[(TRACE|DEBUG|INFO|WARN(?:ING)?|ERROR|FATAL|PANIC)\]` + // bracket style
		`|` +
		`^(TRACE|DEBUG|INFO|WARNI?(?:NG)?|ERROR|FATAL|PANIC|CRITICAL)\s` + // level first
		`)`,
)

// detectEmbeddedSeverityScanLimit bounds how much of a message the detector
// reads. A level marker that appears past the first 200 bytes of a line is
// prose about a level rather than the line's own level.
const detectEmbeddedSeverityScanLimit = 200

// detectEmbeddedSeverity returns the canonical severity a message body declares
// about itself, or the empty string when it declares none.
func detectEmbeddedSeverity(msg string) string {
	check := msg
	if len(check) > detectEmbeddedSeverityScanLimit {
		check = check[:detectEmbeddedSeverityScanLimit]
	}
	m := reEmbeddedLevel.FindStringSubmatch(check)
	if m == nil {
		return ""
	}
	for _, g := range m[1:] {
		if g != "" {
			return parseSeverity(g)
		}
	}
	return ""
}

// messageFieldPriority is the order structured payload keys are tried in. It is
// a fixed list rather than a map walk because the extracted message enters the
// template pattern, which enters the template id.
var messageFieldPriority = []string{"message", "msg", "event", "textPayload", "log", "body"}

// extractMessageFromMap pulls a message out of a structured payload, falling
// back to the payload's own JSON when no known key carries one. The fallback is
// deliberate rather than an empty string: a structured entry with an unusual
// key is still a log line, and dropping its text would silently shrink the
// graph.
func extractMessageFromMap(m map[string]any) string {
	for _, key := range messageFieldPriority {
		if s := extractMessageField(m, key); s != "" {
			return s
		}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return fmt.Sprintf("%v", m)
	}
	return string(b)
}

// extractMessageField reads one key, recursing into a nested object.
func extractMessageField(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	if nested, ok := v.(map[string]any); ok {
		return extractMessageFromMap(nested)
	}
	return ""
}
