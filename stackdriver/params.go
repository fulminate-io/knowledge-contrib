// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"time"
)

// params.go — the COLLECT'S OWN INPUTS: what a caller names and how each is
// validated.
//
// THESE FIELDS RIDE INSIDE `params`, NOT BESIDE IT. The contract's top level is
// the collect id and this block; the framework advertises the contract's own top
// level and splices the schema inferred from this struct in as the `params`
// property. A collector that pushed its inputs up to the top level would tighten
// the contract's own required list, and every collect the client sends without
// that key would be refused before the call as a client bug.
//
// THE ENTRY BOUND IS AN INPUT AND THE MODULE IMPOSES NONE OF ITS OWN. Collector
// traffic is MCP to MCP: the client and this process talk to each other and none
// of what crosses reaches a language model's context, so the reason a cap exists
// elsewhere — a context budget — does not apply. A collect naming no bound reads
// its source to exhaustion, and a caller naming one gets exactly that number
// with no ceiling above it.
//
// THE DIFFERENCE BETWEEN THE TWO IS WHO IS BOUNDING WHAT. max_entries is a
// FILTER the caller chose, on the same footing as the time range and the
// severity floor. A module-side default would drop collected data for a caller
// who asked for none of it, and a module-side ceiling would refuse a request the
// caller is entitled to make; both are this process deciding how much of the
// operator's own logs they may have.

// params is this collector's own input block.
type params struct {
	// Project is the Google Cloud project whose logs are read. REQUIRED: there
	// is no default project, because reading the wrong project's logs silently
	// is worse than refusing.
	Project string `json:"project"`

	// Filter is a Cloud Logging advanced filter expression, appended verbatim to
	// the clauses built from the other fields.
	//
	// IT IS PASSED THROUGH UNESCAPED AND THAT IS DELIBERATE: a caller naming a
	// native filter expression is asking for exactly that, and escaping it would
	// make every expression that uses an operator unusable. Every OTHER value
	// this collector puts in a filter is sanitized and escaped; see filter.go.
	Filter string `json:"filter,omitempty"`

	// LogName narrows the read to one log, short ("stderr") or fully qualified
	// ("projects/other/logs/stdout"). Unlike Filter it is sanitized and escaped.
	LogName string `json:"log_name,omitempty"`

	// Start and End bound the read, as RFC 3339 timestamps. Either may be
	// omitted, which leaves that side of the range open.
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`

	// SeverityMin drops entries below a canonical severity, at the API where it
	// can be expressed and again after normalization where the GKE
	// reclassification may have moved an entry below it.
	SeverityMin string `json:"severity_min,omitempty"`

	// TextFilter keeps only entries whose message contains this text,
	// case-insensitively.
	TextFilter string `json:"text_filter,omitempty"`

	// Fields are exact-match label filters, keyed by the canonical field names
	// (service, host, namespace, pod, container, level) or by a Cloud Logging
	// field path.
	Fields map[string]string `json:"fields,omitempty"`

	// MaxEntries bounds the read to this many entries.
	//
	// OMITTING IT MEANS UNBOUNDED: the read drains Cloud Logging to exhaustion
	// for the time range and filter given. An explicit value must be POSITIVE;
	// zero and negative are refused, because a bound names how many entries to
	// read and neither of those names a number of entries.
	//
	// IT IS A POINTER SO THAT ABSENT AND ZERO STAY DIFFERENT INPUTS. A plain int
	// decodes both into the same value, which would force one of the two to be
	// coerced into the other's meaning — and reading an explicit 0 as "read
	// everything" is the widest possible reading of the value that most plainly
	// means the opposite. Under this repository's standing rule that bad input
	// errors rather than degrading, the caller who sent 0 gets an error naming
	// what they sent.
	MaxEntries *int `json:"max_entries,omitempty"`
}

// toQuery validates the params and turns them into the read this collect
// performs.
//
// EVERY REFUSAL NAMES THE VALUE AND WHAT WOULD HAVE WORKED. A collector is
// driven by a tool call an operator cannot see, so an error that says only
// "invalid" leaves them with nothing to change.
func (p params) toQuery() (logQuery, error) {
	if p.Project == "" {
		return logQuery{}, fmt.Errorf(
			"stackdriver: params.project is required and was empty; " +
				"it names the Google Cloud project whose logs are read, for example \"fulminate-services\"")
	}

	start, err := parseBound("start", p.Start)
	if err != nil {
		return logQuery{}, err
	}
	end, err := parseBound("end", p.End)
	if err != nil {
		return logQuery{}, err
	}
	if !start.IsZero() && !end.IsZero() && end.Before(start) {
		return logQuery{}, fmt.Errorf(
			"stackdriver: params.end (%s) is before params.start (%s); "+
				"the range names no entries and an inverted range is far more often a swapped pair than an intent",
			p.End, p.Start)
	}

	// THE STRICT PARSE IS THE POINT HERE. The lenient one defaults an
	// unrecognized word to INFO, which is right for a level marker scraped out
	// of a log body and wrong for a parameter: an operator who asked to see
	// errors and got everything would have no way to tell that their spelling
	// was not understood.
	severity := ""
	if p.SeverityMin != "" {
		parsed, ok := parseSeverityStrict(p.SeverityMin)
		if !ok {
			return logQuery{}, fmt.Errorf(
				"stackdriver: params.severity_min %q is not a severity; "+
					"the vocabulary is TRACE, DEBUG, INFO, WARN, ERROR, CRITICAL", p.SeverityMin)
		}
		severity = parsed
	}

	bound, err := p.resolvedBound()
	if err != nil {
		return logQuery{}, err
	}

	return logQuery{
		Source:       p.LogName,
		StartTime:    start,
		EndTime:      end,
		TextFilter:   p.TextFilter,
		FieldFilters: p.Fields,
		SeverityMin:  severity,
		MaxEntries:   bound,
		RawQuery:     p.Filter,
	}, nil
}

// resolvedBound returns the entry bound this collect runs under: the caller's
// value when they named a positive one, and ZERO — which the drain reads as
// "to exhaustion" — when they named none at all.
//
// THREE INPUTS, THREE ANSWERS. Absent is unbounded. A positive value is honored
// with no ceiling above it, because a caller bounding their own request is a
// filter rather than this module rationing the operator's logs. Zero and
// negative are REFUSED: neither names a number of entries, and coercing either
// into the unbounded read would be this function deciding that a caller who
// asked for nothing meant everything.
func (p params) resolvedBound() (int, error) {
	if p.MaxEntries == nil {
		return 0, nil
	}
	if *p.MaxEntries <= 0 {
		return 0, fmt.Errorf(
			"stackdriver: params.max_entries is %d; a bound names how many entries to read and must be "+
				"positive, and omitting the field reads the whole time range", *p.MaxEntries)
	}
	return *p.MaxEntries, nil
}

// parseBound parses one side of the time range.
func parseBound(name, value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	ts, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf(
			"stackdriver: params.%s %q is not an RFC 3339 timestamp, "+
				"for example \"2026-09-07T12:00:00Z\": %w", name, value, err)
	}
	return ts, nil
}
