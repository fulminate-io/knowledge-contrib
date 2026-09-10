// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"regexp"
	"strings"
)

// severity.go — the level a template carries, and why it comes from the message
// body rather than from the stream the line arrived on.
//
// THE WRAPPER LIES ON THIS PLATFORM. GKE and similar runtimes surface
// everything a container writes to stderr as ERROR whatever the application
// meant, and other backends coerce to INFO when they extract no structured
// level. So a container's stderr/stdout split is a WEAKER signal than the text
// of the line, and this collector does not use it as the severity wrapper. It
// is not discarded silently either: the `stream` collect parameter selects
// which of the two the read covers, so an operator who wants only stderr asks
// for it, and the level still comes from the body.

// The canonical severity levels, least to most severe.
const (
	SeverityTrace    = "TRACE"
	SeverityDebug    = "DEBUG"
	SeverityInfo     = "INFO"
	SeverityWarn     = "WARN"
	SeverityError    = "ERROR"
	SeverityCritical = "CRITICAL"
)

// severityOrder ranks the canonical levels for comparison. A level outside the
// map ranks 0, below TRACE, which is what makes an unrecognized value lose
// every "is this more severe" test rather than win one.
var severityOrder = map[string]int{
	SeverityTrace:    0,
	SeverityDebug:    1,
	SeverityInfo:     2,
	SeverityWarn:     3,
	SeverityError:    4,
	SeverityCritical: 5,
}

// ParseSeverity normalizes a level string to its canonical form. An
// unrecognized value becomes INFO, which is the log graph's own convention:
// the level is a display and filtering facet, and refusing a line because its
// producer spelled the level unusually would drop data over a cosmetic.
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

// SeverityIndex returns a level's numeric rank.
func SeverityIndex(severity string) int { return severityOrder[severity] }

// SeverityAtLeast reports whether severity is at or above minSeverity.
func SeverityAtLeast(severity, minSeverity string) bool {
	return severityOrder[severity] >= severityOrder[minSeverity]
}

// severityRank is the SECOND ranking in this package and the difference from
// [SeverityIndex] is the whole reason it exists: it puts the UNSET level
// strictly below TRACE, where SeverityIndex necessarily collapses the two onto
// zero because its zero is TRACE's own rank.
//
// It is used where a template's level is RAISED to its most severe member. A
// cluster whose first entry carried no level must still take TRACE from its
// second, and under SeverityIndex it would not: 0 > 0 is false and the template
// would keep an empty level for the rest of the run. Where the question is
// instead "is this at least ERROR", SeverityIndex is the right one and an unset
// level losing to TRACE changes no answer.
func severityRank(sev string) int {
	if sev == "" {
		return -1
	}
	rank, ok := severityOrder[strings.ToUpper(sev)]
	if !ok {
		return -1
	}
	return rank
}

// reEmbeddedLevel matches the level markers application log lines actually
// carry: a key/value level field, zerolog's tab-delimited form, a bracketed
// level, and a level word opening the line.
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

// embeddedSeverityScanLimit bounds how much of a line the level scan reads. A
// level marker that is not in the first couple of hundred characters is not a
// level marker, it is the word "error" inside a message, and scanning a whole
// multi-kilobyte stack trace for it costs time to find a false positive.
const embeddedSeverityScanLimit = 200

// DetectEmbeddedSeverity returns the canonical level a message body declares,
// or the empty string when it declares none.
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

// DeriveSeverity is the level one entry carries: whatever its body declares,
// falling back to INFO.
//
// IT IS DELIBERATELY NOT A FUNCTION OF THE CONTAINER STREAM. See this file's
// opening comment: on this platform stderr is not an error level.
func DeriveSeverity(message string) string {
	if detected := DetectEmbeddedSeverity(message); detected != "" {
		return detected
	}
	return SeverityInfo
}

// ReclassifySeverity rewrites, in place, the level of every entry whose carried
// level is empty or INFO but whose body declares one. It is the same step the
// built-in pipeline runs, kept as a separate pass so an entry that arrives with
// a trustworthy level from somewhere else survives it.
func ReclassifySeverity(entries []Entry) []Entry {
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
