// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// severity.go — the SEVERITY VOCABULARY this collector produces, reproduced to
// the built-in log pipeline's rules rather than invented.
//
// It is reproduced and not imported: the built-in vocabulary lives under
// cmd/knowledge/internal, which the Go toolchain refuses to import from another
// module. Every rule below is the built-in rule; where this collector diverges
// on purpose, the divergence is stated at the site.

// The canonical severity levels, ordered least to most severe. The strings are
// the wire values the log graph carries, so they are the built-in spellings
// exactly — a template's `severity` metadata value is one of these.
const (
	SeverityTrace    = "TRACE"
	SeverityDebug    = "DEBUG"
	SeverityInfo     = "INFO"
	SeverityWarn     = "WARN"
	SeverityError    = "ERROR"
	SeverityCritical = "CRITICAL"
)

// severityOrder ranks the canonical severities for comparison. An unlisted
// value ranks 0, which is what makes a non-standard provider severity sort
// below TRACE rather than crashing a comparison.
var severityOrder = map[string]int{
	SeverityTrace:    0,
	SeverityDebug:    1,
	SeverityInfo:     2,
	SeverityWarn:     3,
	SeverityError:    4,
	SeverityCritical: 5,
}

// ParseSeverity normalizes a severity string to its canonical form, returning
// SeverityInfo for anything unrecognized.
//
// INFO IS THE FALLBACK RATHER THAN AN ERROR, and that is deliberate on this one
// function against the repository's bad-input invariant: the input is a token
// lifted out of somebody else's log line, not a parameter this collector was
// given, so there is no caller to fail. A caller that needs to know whether the
// token was recognized compares the result against the input.
func ParseSeverity(s string) string {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case SeverityTrace:
		return SeverityTrace
	case SeverityDebug, "DBG":
		return SeverityDebug
	case SeverityInfo, "INFORMATION", "NOTICE":
		return SeverityInfo
	case SeverityWarn, "WARNING", "WARNI":
		return SeverityWarn
	case SeverityError, "ERR", "SEVERE", "FATAL":
		return SeverityError
	case SeverityCritical, "CRIT", "ALERT", "EMERGENCY", "EMERG", "PANIC":
		return SeverityCritical
	default:
		return SeverityInfo
	}
}

// SeverityAtLeast reports whether severity is at or above minSeverity.
func SeverityAtLeast(severity, minSeverity string) bool {
	return severityOrder[severity] >= severityOrder[minSeverity]
}

// severityIndex is the numeric rank used when merging templates.
func severityIndex(severity string) int { return severityOrder[severity] }

// severityRank ranks a severity for the TEMPLATE AGGREGATE, which is a
// different scale from severityOrder and reproduced separately because the
// built-in pipeline keeps them separate too.
//
// THE TWO SCALES DISAGREE AND THE DISAGREEMENT IS LOAD-BEARING. severityOrder
// ranks TRACE at 0 and treats an unknown value as 0 as well, so an unknown
// value and TRACE are indistinguishable there. This scale ranks TRACE at 1 and
// an unknown value at 0, so a template seeded from an unknown severity is
// raised by the first TRACE entry that follows. It also folds FATAL, CRITICAL
// and EMERGENCY together and compares after upper-casing, which severityOrder
// does neither of.
func severityRank(sev string) int {
	switch strings.ToUpper(sev) {
	case "TRACE":
		return 1
	case "DEBUG":
		return 2
	case "INFO":
		return 3
	case "WARN", "WARNING":
		return 4
	case "ERROR":
		return 5
	case "FATAL", "CRITICAL", "EMERGENCY":
		return 6
	default:
		return 0
	}
}

// reEmbeddedLevel matches the severity markers log producers embed in the
// message body. It is a package-level compiled value: compiling a regexp per
// call would recompile it once per log entry.
var reEmbeddedLevel = regexp.MustCompile(
	`(?i)` +
		`(?:` +
		`"?level"?\s*[:=]\s*"?(\w+)"?` +
		`|` +
		`\t(trace|debug|info|warn(?:ing)?|error|fatal|panic)\t` +
		`|` +
		`\[(TRACE|DEBUG|INFO|WARN(?:ING)?|ERROR|FATAL|PANIC)\]` +
		`|` +
		`^(TRACE|DEBUG|INFO|WARNI?(?:NG)?|ERROR|FATAL|PANIC|CRITICAL)\s` +
		`)`,
)

// embeddedSeverityScanLimit bounds how much of a message body the level scan
// reads. It is the built-in pipeline's bound and it is a real rule rather than a
// performance note: a marker past this character is NOT detected, so a producer
// that writes its level late in the line is classified from its wrapper instead.
const embeddedSeverityScanLimit = 200

// DetectEmbeddedSeverity reads a severity marker out of a message body,
// returning "" when it finds none. It reads no further than
// embeddedSeverityScanLimit.
func DetectEmbeddedSeverity(msg string) string {
	check := msg
	if len(check) > embeddedSeverityScanLimit {
		check = check[:embeddedSeverityScanLimit]
	}
	m := reEmbeddedLevel.FindStringSubmatch(check)
	if m == nil {
		return ""
	}
	for _, g := range m[1:] {
		if g != "" {
			return ParseSeverity(g)
		}
	}
	return ""
}

// reclassifySeverity re-derives severity from the message body for entries that
// carry no severity or the default INFO.
//
// WHY THE BODY OUTRANKS THE WRAPPER HERE. Container platforms surface
// everything written to stderr at one level, and some backends coerce to INFO
// when they extract no structured level, so for those two cases the body is the
// better signal. An entry that already carries a non-INFO severity is left
// alone, because there the wrapper had something to say.
//
// The slice is mutated in place and returned for call-site convenience.
func reclassifySeverity(entries []LogEntry) []LogEntry {
	for i := range entries {
		s := entries[i].Severity
		if s != "" && s != SeverityInfo {
			continue
		}
		if detected := DetectEmbeddedSeverity(entries[i].Message); detected != "" {
			entries[i].Severity = detected
		}
	}
	return entries
}

// extractMessageFromMap pulls the message text out of a decoded JSON log
// payload, trying the conventional field names in priority order and falling
// back to the payload's own JSON rendering when none is present. The fallback
// is what keeps a structured line that names its message something unexpected
// from clustering as the empty string.
func extractMessageFromMap(m map[string]any) string {
	for _, key := range []string{"message", "msg", "event", "textPayload", "log", "body"} {
		if s := extractField(m, key); s != "" {
			return s
		}
	}
	b, err := json.Marshal(m)
	if err != nil {
		return fmt.Sprintf("%v", m)
	}
	return string(b)
}

// extractField reads one string field out of a decoded payload, recursing when
// the value is itself an object.
func extractField(m map[string]any, key string) string {
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
