// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"regexp"
	"strings"
)

// severity.go — the canonical severity vocabulary and the two classifiers that
// read it off a log line.
//
// THE STRINGS ARE PART OF THE GRAPH. A template node carries its severity in
// metadata verbatim and its alias suffix is derived from it, so a collector
// that spelled these differently would produce a graph that validates and
// disagrees with the built-in family on every template.

// The canonical severity levels, ordered least to most severe.
const (
	SeverityTrace    = "TRACE"
	SeverityDebug    = "DEBUG"
	SeverityInfo     = "INFO"
	SeverityWarn     = "WARN"
	SeverityError    = "ERROR"
	SeverityCritical = "CRITICAL"
)

// severityOrder ranks the canonical levels. A value outside the vocabulary
// ranks zero, which is BELOW trace: an unknown severity never wins a
// severity-raise comparison, and never satisfies a minimum.
var severityOrder = map[string]int{
	SeverityTrace:    0,
	SeverityDebug:    1,
	SeverityInfo:     2,
	SeverityWarn:     3,
	SeverityError:    4,
	SeverityCritical: 5,
}

// ParseSeverity normalizes a provider's severity spelling to the canonical
// vocabulary. An unrecognized value becomes INFO rather than an error: a
// severity is a hint carried on a log line, not operator input, and refusing a
// collect over one line's unusual level word would be the wrong trade.
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

// SeverityIndex is the numeric rank of a canonical severity.
func SeverityIndex(severity string) int { return severityOrder[severity] }

// severityRank ranks a RAW severity spelling for the template severity-raise
// comparison. It is deliberately a different function from SeverityIndex: the
// raise compares whatever an entry carries against whatever the template
// carries, and both may be non-canonical.
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

// reEmbeddedLevel matches the level markers platforms bury in a message body.
// A platform that marks every stderr line ERROR makes the wrapper's severity
// useless, so the body is the better signal when the wrapper says nothing or
// says INFO.
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

// DetectEmbeddedSeverity returns the canonical severity a message body declares
// about itself, or the empty string when it declares none. Only the first 200
// bytes are examined: a level marker that far into a line is not a level marker.
func DetectEmbeddedSeverity(msg string) string {
	check := msg
	if len(check) > 200 {
		check = check[:200]
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
