// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"time"
)

// params.go — WHAT A COLLECT SAYS, and where it says it.
//
// These fields are declared INSIDE the contract's `params` object, not beside
// it: the contract's own input requires only the collect id and describes
// `params` as an opaque object whose shape the provider declares. The framework
// infers the schema below from this type and splices it in, so the client
// refuses a malformed collect against THIS declaration before the tool is
// called.
//
// A FIELD WITHOUT `omitempty` IS ADVERTISED AS REQUIRED. LogGroups is the only
// one: it is what this collector cannot run without, and every other field has
// a defined meaning when absent.

// Params is this collector's collect parameters.
type Params struct {
	// LogGroups names the CloudWatch log groups to walk. REQUIRED, and at
	// least one non-empty name: a collect that names no group has no source.
	LogGroups []string `json:"log_groups"`

	// StartTime and EndTime bound the walk, as RFC 3339 timestamps.
	//
	// BOTH ARE OPTIONAL AND THAT IS A DECISION, not an omission. CloudWatch
	// treats an absent bound as the group's whole retained history, which is a
	// legitimate collect of a small group, and a walk of it is COMPLETE — the
	// completeness assertion is about whether the walk enumerated its source,
	// and a narrower window is a smaller source rather than a partial walk.
	StartTime string `json:"start_time,omitempty"`
	EndTime   string `json:"end_time,omitempty"`

	// FilterPattern is a CloudWatch filter pattern, applied SERVER-SIDE by
	// CloudWatch itself. It is the provider-native form and takes precedence
	// over TextFilter, which is applied here.
	FilterPattern string `json:"filter_pattern,omitempty"`

	// TextFilter keeps only entries whose message contains this text,
	// case-insensitively. It is applied after normalization, so it matches the
	// unwrapped message rather than the raw line — which is why it cannot be
	// pushed into FilterPattern.
	TextFilter string `json:"text_filter,omitempty"`

	// SeverityMin keeps only entries at or above this severity. It is applied
	// here and not by CloudWatch, which cannot order severity levels.
	SeverityMin string `json:"severity_min,omitempty"`

	// MaxEntries bounds how many entries the walk collects across every named
	// group. Reaching it makes the walk INCOMPLETE, because entries the source
	// held were not enumerated.
	//
	// THERE IS NO DEFAULT AND NO CEILING. Absent means drain every named group
	// to exhaustion, which is what a collect with no opinion gets; any positive
	// value a caller names is honored whatever its size, because this result
	// travels between two programs and never reaches a language model, so
	// nothing downstream needs it small.
	//
	// IT IS A POINTER SO THAT ABSENT AND AN EXPLICIT ZERO ARE DIFFERENT
	// ANSWERS, and that is the whole reason for the indirection. A plain int
	// decodes both to zero, so a caller who wrote `"max_entries": 0` — asking
	// for no entries, which means nothing — would be indistinguishable from a
	// caller who wrote nothing and gets an unbounded walk. Coercing the first
	// into the second is exactly the silent coercion this repository forbids.
	// Absent is unbounded; an explicit zero or a negative value is refused.
	MaxEntries *int `json:"max_entries,omitempty"`

	// Region is the AWS region to query. Absent means the region the default
	// chain resolves, which is what an operator with a configured profile or
	// an instance role expects.
	Region string `json:"region,omitempty"`
}

// window is the validated, decoded form of the two time bounds.
type window struct {
	Start time.Time
	End   time.Time
}

// entryBound is the resolved entry bound: the caller's positive value, or ZERO
// meaning unbounded.
//
// It is only meaningful after validate has run, which is what guarantees that a
// non-nil value is positive. Zero is safe to use as the unbounded sentinel HERE
// precisely because validate refuses an explicit zero at the boundary: the
// ambiguity is resolved once, at the edge, so the walk below can carry a plain
// int without re-deciding it.
func (p Params) entryBound() int {
	if p.MaxEntries == nil {
		return 0
	}
	return *p.MaxEntries
}

// validate checks the parameters a schema cannot and returns the decoded
// window.
//
// THE SCHEMA CANNOT SEE THREE OF THESE. It can require log_groups to be
// present, but not that its entries are non-empty; it can require the
// timestamps to be strings, but not that they parse; and it cannot compare two
// fields, so an end before its start reaches here. Each is refused by name.
func (p Params) validate() (window, error) {
	if len(p.LogGroups) == 0 {
		return window{}, fmt.Errorf("cloudwatch: no log group was named; set params.log_groups to at least one log group")
	}
	for i, g := range p.LogGroups {
		if g == "" {
			return window{}, fmt.Errorf("cloudwatch: params.log_groups[%d] is empty; every entry must name a log group", i)
		}
	}
	if p.MaxEntries != nil && *p.MaxEntries <= 0 {
		return window{}, fmt.Errorf(
			"cloudwatch: params.max_entries is %d; omit the key entirely to walk every named log group to "+
				"exhaustion, or name a positive number of entries. Zero is refused rather than read as "+
				"unbounded: a caller who asked for no entries and a caller who asked for no bound have said "+
				"two different things", *p.MaxEntries)
	}
	if p.SeverityMin != "" {
		if _, ok := severityOrder[p.SeverityMin]; !ok {
			return window{}, fmt.Errorf(
				"cloudwatch: params.severity_min is %q, which is not one of TRACE, DEBUG, INFO, WARN, ERROR, CRITICAL",
				p.SeverityMin)
		}
	}

	var w window
	var err error
	if w.Start, err = parseBound("start_time", p.StartTime); err != nil {
		return window{}, err
	}
	if w.End, err = parseBound("end_time", p.EndTime); err != nil {
		return window{}, err
	}
	if !w.Start.IsZero() && !w.End.IsZero() && w.End.Before(w.Start) {
		return window{}, fmt.Errorf(
			"cloudwatch: params.end_time %s is before params.start_time %s, so the window selects nothing",
			p.EndTime, p.StartTime)
	}
	return w, nil
}

// parseBound decodes one RFC 3339 bound, treating the empty string as unset.
func parseBound(name, value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	ts, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("cloudwatch: params.%s %q is not an RFC 3339 timestamp: %w", name, value, err)
	}
	return ts, nil
}
