// SPDX-License-Identifier: Apache-2.0

package correlation

// severity.go — THE RANK THE ERROR FILTER READS.
//
// WHY THE VOCABULARY IS HERE AND NOT AT THE CALLER. "Templates at ERROR or
// above" is part of the detector's specification, not of any collector's: it is
// what stops correlation from pairing every heartbeat in a collect with every
// other one. A caller that filtered first would own that rule, and two callers
// filtering differently is exactly the drift this module exists to end. So the
// filter lives here, and the rank it reads lives here with it.
//
// THE SIX NAMES ARE THE EMITTED GRAPH'S OWN and each logs collector already
// writes them into a template's `severity` metadata; a collector whose source
// speaks another vocabulary normalizes into these before it builds a Template,
// exactly as it already does before it emits one. An UNMAPPED NAME RANKS ZERO,
// below TRACE, so it never satisfies a minimum: an unknown level is not evidence
// of an error.
const (
	SeverityTrace    = "TRACE"
	SeverityDebug    = "DEBUG"
	SeverityInfo     = "INFO"
	SeverityWarn     = "WARN"
	SeverityError    = "ERROR"
	SeverityCritical = "CRITICAL"
)

// severityOrder is the comparison order, least to most severe.
var severityOrder = map[string]int{
	SeverityTrace:    0,
	SeverityDebug:    1,
	SeverityInfo:     2,
	SeverityWarn:     3,
	SeverityError:    4,
	SeverityCritical: 5,
}

// SeverityAtLeast reports whether severity ranks at or above minSeverity. It is
// exported so an importing collector can assert that ITS vocabulary and this
// one rank identically, which is the only thing standing between the two
// spellings drifting apart unnoticed.
func SeverityAtLeast(severity, minSeverity string) bool {
	return severityOrder[severity] >= severityOrder[minSeverity]
}
