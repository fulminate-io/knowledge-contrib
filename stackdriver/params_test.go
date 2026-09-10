// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"strings"
	"testing"
)

// params_test.go — one arm per refusal, each checked for naming the offending
// value AND what would have worked. An error that says only "invalid" leaves an
// operator with nothing to change, because the tool call they would fix is one
// they never see.

func TestParamsRefusals(t *testing.T) {
	for _, tc := range []struct {
		name       string
		in         params
		wantInText []string
	}{
		{
			name:       "no project",
			in:         params{},
			wantInText: []string{"project", "required", "fulminate-services"},
		},
		{
			name:       "unparseable start",
			in:         params{Project: "p", Start: "yesterday"},
			wantInText: []string{"start", "yesterday", "RFC 3339", "2026-09-07T12:00:00Z"},
		},
		{
			name:       "unparseable end",
			in:         params{Project: "p", End: "soon"},
			wantInText: []string{"end", "soon", "RFC 3339"},
		},
		{
			name:       "inverted range",
			in:         params{Project: "p", Start: "2026-09-07T13:00:00Z", End: "2026-09-07T12:00:00Z"},
			wantInText: []string{"before", "2026-09-07T12:00:00Z", "2026-09-07T13:00:00Z"},
		},
		{
			name:       "unknown severity",
			in:         params{Project: "p", SeverityMin: "LOUD"},
			wantInText: []string{"severity_min", "LOUD", "TRACE, DEBUG, INFO, WARN, ERROR, CRITICAL"},
		},
		{
			// A bound names how many entries to read, so a non-positive one
			// names nothing. Bad input errors rather than being coerced into
			// some other meaning — here, into the unbounded read that OMITTING
			// the field already expresses.
			name:       "negative bound",
			in:         params{Project: "p", MaxEntries: new(-1)},
			wantInText: []string{"max_entries", "-1", "positive", "omitting"},
		},
		{
			// AN EXPLICIT ZERO IS NOT AN ABSENT FIELD. A caller who sent
			// max_entries: 0 asked for something, and reading it as "everything"
			// is the widest possible reading of a value that most plainly means
			// "nothing". Absent and zero are different inputs and get different
			// answers, which is why the field is a pointer.
			name:       "explicit zero bound",
			in:         params{Project: "p", MaxEntries: new(0)},
			wantInText: []string{"max_entries", "0", "positive", "omitting"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.in.toQuery()
			if err == nil {
				t.Fatalf("%+v was accepted", tc.in)
			}
			for _, want := range tc.wantInText {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("the refusal does not mention %q: %v", want, err)
				}
			}
		})
	}
}

// TestParamsDefaults covers what an omitted field means, which is the other half
// of every refusal above.
func TestParamsDefaults(t *testing.T) {
	q, err := params{Project: "p"}.toQuery()
	if err != nil {
		t.Fatalf("the minimal params were refused: %v", err)
	}
	if q.MaxEntries != 0 {
		t.Errorf("bound = %d, want 0, which is the unbounded read an omitted field means", q.MaxEntries)
	}
	if !q.StartTime.IsZero() || !q.EndTime.IsZero() {
		t.Errorf("an omitted range produced bounds %v..%v", q.StartTime, q.EndTime)
	}
	if q.SeverityMin != "" {
		t.Errorf("an omitted severity became %q", q.SeverityMin)
	}
}

// TestTheModuleImposesNoBoundOfItsOwn is the inversion of a test this file used
// to carry, which asserted that an omitted bound became a real number.
//
// COLLECTOR TRAFFIC CARRIES NO CAP, so a module-side default and a module-side
// ceiling are both this process deciding how much of the operator's own logs
// they may have. This asserts the absence directly rather than only through the
// drain: no positive value is reachable from params that name no bound, at any
// combination of the other fields.
func TestTheModuleImposesNoBoundOfItsOwn(t *testing.T) {
	for _, in := range []params{
		{Project: "p"},
		{Project: "p", Filter: "severity >= ERROR"},
		{Project: "p", Start: "2026-09-07T12:00:00Z", End: "2026-09-07T13:00:00Z"},
		{Project: "p", SeverityMin: "ERROR", TextFilter: "timeout"},
	} {
		q, err := in.toQuery()
		if err != nil {
			t.Fatalf("%+v was refused: %v", in, err)
		}
		if q.MaxEntries != 0 {
			t.Errorf("%+v produced the bound %d; the module imposes none", in, q.MaxEntries)
		}
	}

	// SAME-RUN CONTROL: the field still WORKS when the caller names it, so the
	// zeros above are an absent default rather than an ignored input.
	q, err := params{Project: "p", MaxEntries: new(7)}.toQuery()
	if err != nil {
		t.Fatalf("a named bound was refused: %v", err)
	}
	if q.MaxEntries != 7 {
		t.Fatalf("a named bound of 7 became %d", q.MaxEntries)
	}
}

// TestAnAbsentBoundAndAnExplicitZeroAreDifferentInputs is the distinction the
// pointer exists for, asserted end to end from the JSON a caller actually sends.
//
// A PLAIN int CANNOT EXPRESS THIS. Both an absent key and an explicit zero
// decode into the same Go zero value, so a module holding an int has already
// lost the difference before any rule can be applied to it and would be forced
// to coerce one of the two. Driving the decode here rather than building the
// struct is what makes that a property of the wire shape rather than of a
// convention this file follows.
func TestAnAbsentBoundAndAnExplicitZeroAreDifferentInputs(t *testing.T) {
	decode := func(body string) params {
		t.Helper()
		var p params
		if err := json.Unmarshal([]byte(body), &p); err != nil {
			t.Fatalf("decoding %s: %v", body, err)
		}
		return p
	}

	absent := decode(`{"project":"p"}`)
	if absent.MaxEntries != nil {
		t.Errorf("an absent max_entries decoded to %d, want nil", *absent.MaxEntries)
	}
	q, err := absent.toQuery()
	if err != nil {
		t.Fatalf("an absent bound was refused: %v", err)
	}
	if q.MaxEntries != 0 {
		t.Errorf("an absent bound produced %d, want the unbounded read", q.MaxEntries)
	}

	zero := decode(`{"project":"p","max_entries":0}`)
	if zero.MaxEntries == nil {
		t.Fatalf("an explicit max_entries of 0 decoded to nil, so it is indistinguishable from absent")
	}
	if _, err := zero.toQuery(); err == nil {
		t.Errorf("an explicit zero bound was accepted")
	}
}

// TestParamsNormalizesSeverityBeforeUse covers the accepted spellings, so a
// caller writing "warning" or "warn" reaches the same clause.
func TestParamsNormalizesSeverityBeforeUse(t *testing.T) {
	for _, spelling := range []string{"WARN", "warn", "warning", "WARNING"} {
		q, err := params{Project: "p", SeverityMin: spelling}.toQuery()
		if err != nil {
			t.Fatalf("%q was refused: %v", spelling, err)
		}
		if q.SeverityMin != severityWarn {
			t.Errorf("%q normalized to %q, want %q", spelling, q.SeverityMin, severityWarn)
		}
	}
}

// TestTheRawFilterIsCarriedVerbatim pins the one deliberate pass-through, so a
// later reader can tell it from the sanitized fields.
func TestTheRawFilterIsCarriedVerbatim(t *testing.T) {
	raw := `jsonPayload.code >= 500 AND labels."k8s-pod/app" = "checkout"`
	q, err := params{Project: "p", Filter: raw}.toQuery()
	if err != nil {
		t.Fatalf("toQuery: %v", err)
	}
	if q.RawQuery != raw {
		t.Errorf("the filter was altered:\n got: %s\nwant: %s", q.RawQuery, raw)
	}
	if !strings.Contains(buildFilter("p", q), raw) {
		t.Errorf("the raw filter did not reach the built filter verbatim")
	}
}
