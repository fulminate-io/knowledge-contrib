// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// bound_test.go — THE ONE CALLER-FACING BOUND this collector has, in every state
// it can be in: absent, an explicit zero, negative, positive, and larger than
// the transport's own request field. The rest of params validation is in
// params_test.go.

// TestAnExplicitZeroOrNegativeEntryBoundIsRefused is the bad-input arm for the
// one caller-named bound this collector has, in all four states.
//
// AN EXPLICIT ZERO AND AN ABSENT KEY ARE DIFFERENT ANSWERS. Absent means drain
// the source to exhaustion; an explicit zero asks for no entries, which means
// nothing, and reading it as "unbounded" would be a silent coercion. Telling
// them apart is why the field is a pointer: a plain int decodes both to zero and
// the distinction cannot be recovered afterwards.
func TestAnExplicitZeroOrNegativeEntryBoundIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value int
	}{
		{"an explicit zero", 0},
		{"a negative bound", -1},
		{"a large negative bound", -5000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := tc.value
			_, err := Params{LogGroups: []string{"/g"}, MaxEntries: &v}.validate()
			if err == nil {
				t.Fatalf("max_entries %d was accepted", tc.value)
			}
			if !strings.Contains(err.Error(), "max_entries") {
				t.Errorf("the refusal does not name the parameter: %v", err)
			}
		})
	}

	// THE TWO CONTROLS, so the refusals above are attributable to the VALUE and
	// not to the parameter being rejected generally.
	t.Run("absent is accepted and unbounded", func(t *testing.T) {
		p := Params{LogGroups: []string{"/g"}}
		if _, err := p.validate(); err != nil {
			t.Fatalf("an ABSENT max_entries was refused: %v", err)
		}
		if got := p.entryBound(); got != 0 {
			t.Errorf("the resolved bound is %d, want 0 meaning unbounded", got)
		}
	})
	t.Run("a positive bound is accepted and resolved", func(t *testing.T) {
		v := 5
		p := Params{LogGroups: []string{"/g"}, MaxEntries: &v}
		if _, err := p.validate(); err != nil {
			t.Fatalf("a positive max_entries was refused: %v", err)
		}
		if got := p.entryBound(); got != 5 {
			t.Errorf("the resolved bound is %d, want 5", got)
		}
	})
}

// TestAnAbsentMaxEntriesDrainsToExhaustion pins the default: no bound, no
// ceiling, and the walk follows the cursor to its end over every named group.
func TestAnAbsentMaxEntriesDrainsToExhaustion(t *testing.T) {
	groups := pagedGroups([]recordedEvent{ev("1", 1772366774000, "alpha one")},
		[]recordedEvent{ev("2", 1772366780000, "alpha two")})
	client := newFakeClientFor(t, groups)
	result, err := collectorWithFake(client).Walk(context.Background(), "id", Params{LogGroups: []string{"/g"}}, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	if client.calls != 2 {
		t.Errorf("the walk made %d requests, want 2; an absent bound drains the cursor", client.calls)
	}
	if !result.Complete.IsComplete() {
		t.Errorf("an unbounded walk asserted INCOMPLETE (%q)", result.Complete.Reason())
	}
	if got := countChunkEntries(result); got != 2 {
		t.Errorf("the unbounded walk kept %d entries, want both", got)
	}
}

// TestABoundIsTruncationONLYWhenItWasReached is the measured-not-inferred cell,
// asserted in BOTH directions in one test because either alone admits a
// constant.
//
// A collector that reported truncation whenever a bound was NAMED would make
// every collect with a generous bound incomplete forever, and the server's
// deletion phase would never run again — with nothing anywhere going red.
func TestABoundIsTruncationONLYWhenItWasReached(t *testing.T) {
	groups := pagedGroups([]recordedEvent{ev("1", 1772366774000, "alpha one")},
		[]recordedEvent{ev("2", 1772366780000, "alpha two")})

	t.Run("a generous bound that was never reached leaves the walk COMPLETE", func(t *testing.T) {
		v := 1000
		result, err := collectorWithFake(newFakeClientFor(t, groups)).Walk(context.Background(), "id", Params{LogGroups: []string{"/g"}, MaxEntries: &v}, framework.ForeignContext{})
		if err != nil {
			t.Fatalf("walk: %v", err)
		}
		if !result.Complete.IsComplete() {
			t.Errorf("a walk that read 2 entries under a bound of %d asserted INCOMPLETE (%q); truncation is "+
				"measured against what the read returned, never inferred from a bound being set",
				v, result.Complete.Reason())
		}
	})

	// THE BOUNDARY CELL: the bound EQUALS the source size. The read stops
	// because the budget is spent AND the cursor is exhausted, so the walk
	// enumerated its whole source and is complete. A predicate that decides
	// truncation from the budget rather than from the cursor reports INCOMPLETE
	// here, and an operator whose bound sits at a steady-state group's size
	// then never gets a deletion phase again.
	t.Run("a bound EQUAL to the source size leaves the walk COMPLETE", func(t *testing.T) {
		v := 2
		result, err := collectorWithFake(newFakeClientFor(t, groups)).Walk(
			context.Background(), "id", Params{LogGroups: []string{"/g"}, MaxEntries: &v}, framework.ForeignContext{})
		if err != nil {
			t.Fatalf("walk: %v", err)
		}
		if !result.Complete.IsComplete() {
			t.Errorf("a walk that read all 2 of its source's entries under a bound of 2 asserted INCOMPLETE "+
				"(%q); the bound was spent, but nothing was left behind", result.Complete.Reason())
		}
		if got := countChunkEntries(result); got != 2 {
			t.Errorf("the walk kept %d entries, want both", got)
		}
	})

	t.Run("a bound one BELOW the source size makes it INCOMPLETE", func(t *testing.T) {
		v := 1
		result, err := collectorWithFake(newFakeClientFor(t, groups)).Walk(context.Background(), "id", Params{LogGroups: []string{"/g"}, MaxEntries: &v}, framework.ForeignContext{})
		if err != nil {
			t.Fatalf("walk: %v", err)
		}
		if result.Complete.IsComplete() {
			t.Fatal("a walk cut off at its bound asserted COMPLETE")
		}
		if !strings.Contains(result.Complete.Reason(), "max_entries") {
			t.Errorf("the reason %q does not name the bound that cut the walk", result.Complete.Reason())
		}
	})
}

// TestALargeCallerNamedBoundIsHonoredNotClamped pins that there is no ceiling.
//
// THE FAILURE IT CATCHES IS A NARROWING CONVERSION, not a policy: the per-request
// limit is a 32-bit field, so a bound past that width converted before it is
// compared wraps to a negative page limit and the provider refuses the request.
// A caller may name any bound.
func TestALargeCallerNamedBoundIsHonoredNotClamped(t *testing.T) {
	huge := 3_000_000_000
	groups := []recordedGroup{{LogGroup: "/g", Pages: []recordedPage{{
		Events: []recordedEvent{ev("1", 1772366774000, "alpha one")},
	}}}}
	client := newFakeClientFor(t, groups)
	result, err := collectorWithFake(client).Walk(context.Background(), "id", Params{LogGroups: []string{"/g"}, MaxEntries: &huge}, framework.ForeignContext{})
	if err != nil {
		t.Fatalf("a bound of %d was refused: %v", huge, err)
	}
	if len(client.limits) == 0 {
		t.Fatal("the walk made no request")
	}
	if client.limits[0] != pageLimit {
		t.Errorf("the first request asked for %d events, want the full page of %d; a bound larger than a "+
			"page must leave the page size alone rather than wrap it", client.limits[0], pageLimit)
	}
	if !result.Complete.IsComplete() {
		t.Errorf("a walk far under its bound asserted INCOMPLETE (%q)", result.Complete.Reason())
	}
}
