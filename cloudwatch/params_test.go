// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// params_test.go — the checks a JSON Schema cannot make, each refused by name.

// TestParamsValidationRefusesWhatTheSchemaCannotSee covers the three arms the
// advertised schema is blind to, plus the arms it does cover so the pair reads
// as a whole.
func TestParamsValidationRefusesWhatTheSchemaCannotSee(t *testing.T) {
	for _, tc := range []struct {
		name   string
		params Params
		names  string
	}{
		{
			// The schema can require the key; it cannot require the list to be
			// non-empty.
			"an empty log-group list", Params{LogGroups: nil}, "log_groups",
		},
		{
			// Nor that each entry names something.
			"an empty log-group name", Params{LogGroups: []string{"/a", ""}}, "log_groups[1]",
		},
		{
			// The schema can require a string; it cannot require it to parse.
			"an unparseable start time",
			Params{LogGroups: []string{"/a"}, StartTime: "yesterday"}, "start_time",
		},
		{
			"an unparseable end time",
			Params{LogGroups: []string{"/a"}, EndTime: "2026-13-45"}, "end_time",
		},
		{
			// The schema cannot compare two fields at all.
			"an end before its start",
			Params{
				LogGroups: []string{"/a"},
				StartTime: "2026-03-01T12:00:00Z",
				EndTime:   "2026-03-01T11:00:00Z",
			}, "end_time",
		},
		{
			"a severity that is not a level",
			Params{LogGroups: []string{"/a"}, SeverityMin: "LOUD"}, "severity_min",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.params.validate()
			if err == nil {
				t.Fatalf("%s was accepted", tc.name)
			}
			if !strings.Contains(err.Error(), tc.names) {
				t.Errorf("the refusal does not name %q: %v", tc.names, err)
			}
		})
	}
}

// TestValidParamsDecodeTheWindow is the positive half: the bounds parse, an
// absent bound is the zero instant, and a valid collect is accepted.
func TestValidParamsDecodeTheWindow(t *testing.T) {
	w, err := Params{
		LogGroups: []string{"/a"},
		StartTime: "2026-03-01T12:00:00Z",
		EndTime:   "2026-03-01T13:00:00Z",
	}.validate()
	if err != nil {
		t.Fatalf("a valid collect was refused: %v", err)
	}
	if !w.Start.Equal(time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("start = %s, want the parsed bound", w.Start)
	}
	if !w.End.Equal(time.Date(2026, 3, 1, 13, 0, 0, 0, time.UTC)) {
		t.Errorf("end = %s, want the parsed bound", w.End)
	}

	// BOTH BOUNDS ARE OPTIONAL, and an absent one means the group's whole
	// retained history rather than a defaulted window.
	w, err = Params{LogGroups: []string{"/a"}}.validate()
	if err != nil {
		t.Fatalf("a collect with no window was refused: %v", err)
	}
	if !w.Start.IsZero() || !w.End.IsZero() {
		t.Errorf("an absent window decoded to %s..%s, want two zero instants", w.Start, w.End)
	}
}

// TestTheWindowReachesTheRequest pins that the decoded bounds are what the
// CloudWatch request carries, in the API's own millisecond form.
func TestTheWindowReachesTheRequest(t *testing.T) {
	groups := []recordedGroup{{LogGroup: "/g", Pages: []recordedPage{{}}}}
	client := newFakeClientFor(t, groups)
	client.capture = true

	params := Params{
		LogGroups:     []string{"/g"},
		StartTime:     "2026-03-01T12:00:00Z",
		EndTime:       "2026-03-01T13:00:00Z",
		FilterPattern: `"upstream"`,
	}
	if _, err := collectorWithFake(client).Walk(context.Background(), "id", params, framework.ForeignContext{}); err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(client.inputs) != 1 {
		t.Fatalf("the walk made %d requests, want 1", len(client.inputs))
	}
	in := client.inputs[0]
	if in.StartTime == nil || *in.StartTime != time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC).UnixMilli() {
		t.Errorf("the request's start time is %v, want the window's start in milliseconds", in.StartTime)
	}
	if in.EndTime == nil || *in.EndTime != time.Date(2026, 3, 1, 13, 0, 0, 0, time.UTC).UnixMilli() {
		t.Errorf("the request's end time is %v, want the window's end in milliseconds", in.EndTime)
	}
	// THE FILTER PATTERN IS PASSED THROUGH VERBATIM: it is CloudWatch's own
	// syntax and this collector does not rewrite it.
	if in.FilterPattern == nil || *in.FilterPattern != `"upstream"` {
		t.Errorf("the request's filter pattern is %v, want the caller's own pattern verbatim", in.FilterPattern)
	}
}

// TestTheFirstRequestShrinksToASmallCap pins the batching decision: a collect
// asking for fewer entries than a full page makes one small request instead of
// pulling ten thousand events to keep a handful.
func TestTheFirstRequestShrinksToASmallCap(t *testing.T) {
	groups := []recordedGroup{{LogGroup: "/g", Pages: []recordedPage{{
		Events: []recordedEvent{ev("1", 1772366774000, "alpha one")},
	}}}}

	three := 3
	client := newFakeClientFor(t, groups)
	if _, err := collectorWithFake(client).Walk(context.Background(), "id", Params{LogGroups: []string{"/g"}, MaxEntries: &three}, framework.ForeignContext{}); err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(client.limits) == 0 || client.limits[0] != 3 {
		t.Errorf("the first request asked for %v events, want the remaining cap of 3", client.limits)
	}

	// The control: with no cap the request asks for a full page.
	client = newFakeClientFor(t, groups)
	if _, err := collectorWithFake(client).Walk(context.Background(), "id", Params{LogGroups: []string{"/g"}}, framework.ForeignContext{}); err != nil {
		t.Fatalf("walk: %v", err)
	}
	if len(client.limits) == 0 || client.limits[0] != pageLimit {
		t.Errorf("the uncapped request asked for %v events, want the full page of %d", client.limits, pageLimit)
	}
}
