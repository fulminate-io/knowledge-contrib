// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// bound_pages_test.go — the equal-bound boundary in every page shape a paged
// source can present it, and the same question across log groups. The bound's
// other states are in bound_test.go.

// TestTheEqualBoundCellHoldsAcrossPageShapes drives the equal-bound boundary in
// the two shapes a paged source can present it, because the two reach different
// code: the budget can land in the middle of a page, or exactly on a page
// boundary with a live cursor beyond it.
func TestTheEqualBoundCellHoldsAcrossPageShapes(t *testing.T) {
	t.Run("the budget lands on a page boundary and the cursor is exhausted", func(t *testing.T) {
		// Two pages of one event each: the budget of 2 is spent at the end of
		// the second page, whose token is nil.
		groups := pagedGroups([]recordedEvent{ev("1", 1772366774000, "alpha one")},
			[]recordedEvent{ev("2", 1772366780000, "alpha two")})
		v := 2
		result, err := collectorWithFake(newFakeClientFor(t, groups)).Walk(
			context.Background(), "id", Params{LogGroups: []string{"/g"}, MaxEntries: &v}, framework.ForeignContext{})
		if err != nil {
			t.Fatalf("walk: %v", err)
		}
		if !result.Complete.IsComplete() {
			t.Errorf("asserted INCOMPLETE (%q) though the cursor was exhausted", result.Complete.Reason())
		}
	})

	t.Run("the budget lands on a page boundary and the cursor still holds an entry", func(t *testing.T) {
		// The same two pages plus a third the budget never reaches. Here the
		// walk MUST report incomplete, and only a probe past the boundary can
		// tell this cell from the one above.
		groups := []recordedGroup{{LogGroup: "/g", Pages: []recordedPage{
			{NextToken: "p2", Events: []recordedEvent{ev("1", 1772366774000, "alpha one")}},
			{NextToken: "p3", Events: []recordedEvent{ev("2", 1772366780000, "alpha two")}},
			{Events: []recordedEvent{ev("3", 1772366790000, "alpha three")}},
		}}}
		v := 2
		result, err := collectorWithFake(newFakeClientFor(t, groups)).Walk(
			context.Background(), "id", Params{LogGroups: []string{"/g"}, MaxEntries: &v}, framework.ForeignContext{})
		if err != nil {
			t.Fatalf("walk: %v", err)
		}
		if result.Complete.IsComplete() {
			t.Fatal("asserted COMPLETE though a further page held an entry")
		}
	})

	t.Run("an empty page beyond the boundary does not count as more data", func(t *testing.T) {
		// A live cursor pointing at a page with no events, then exhaustion. The
		// source held nothing further, so the walk is complete — a probe that
		// stopped at "there is a token" would get this wrong.
		groups := []recordedGroup{{LogGroup: "/g", Pages: []recordedPage{
			{NextToken: "p2", Events: []recordedEvent{
				ev("1", 1772366774000, "alpha one"), ev("2", 1772366780000, "alpha two")}},
			{},
		}}}
		v := 2
		result, err := collectorWithFake(newFakeClientFor(t, groups)).Walk(
			context.Background(), "id", Params{LogGroups: []string{"/g"}, MaxEntries: &v}, framework.ForeignContext{})
		if err != nil {
			t.Fatalf("walk: %v", err)
		}
		if !result.Complete.IsComplete() {
			t.Errorf("asserted INCOMPLETE (%q) though the only page beyond the bound was empty",
				result.Complete.Reason())
		}
	})

	// THE EMPTY PAGE WITH A LIVE CURSOR, which is what makes the probe a LOOP
	// rather than a single request. A paged API may answer with no events and a
	// cursor that still points at data, so a probe that stopped at the first
	// empty answer would report a complete walk over a source that held more —
	// and every other cell in this file has at least one event on every page,
	// so none of them can tell a loop from a single probe.
	t.Run("an empty page BETWEEN the bound and the remaining data is followed", func(t *testing.T) {
		groups := []recordedGroup{{LogGroup: "/g", Pages: []recordedPage{
			{NextToken: "p2", Events: []recordedEvent{
				ev("1", 1772366774000, "alpha one"), ev("2", 1772366780000, "alpha two")}},
			{NextToken: "p3"},
			{Events: []recordedEvent{ev("3", 1772366790000, "alpha three")}},
		}}}
		v := 2
		result, err := collectorWithFake(newFakeClientFor(t, groups)).Walk(
			context.Background(), "id", Params{LogGroups: []string{"/g"}, MaxEntries: &v}, framework.ForeignContext{})
		if err != nil {
			t.Fatalf("walk: %v", err)
		}
		if result.Complete.IsComplete() {
			t.Fatal("asserted COMPLETE though an entry sat one empty page past the bound; the cursor probe " +
				"must follow an empty page rather than stopping at it")
		}
	})

	t.Run("an entry beyond the bound that the FILTER excludes is not more data", func(t *testing.T) {
		// The cursor holds an entry, but this collect would have dropped it, so
		// the bound cut nothing off. A probe that did not apply the filters
		// would report a truncated walk here.
		groups := []recordedGroup{{LogGroup: "/g", Pages: []recordedPage{
			{NextToken: "p2", Events: []recordedEvent{
				ev("1", 1772366774000, "keep alpha"), ev("2", 1772366780000, "keep beta")}},
			{Events: []recordedEvent{ev("3", 1772366790000, "drop gamma")}},
		}}}
		v := 2
		result, err := collectorWithFake(newFakeClientFor(t, groups)).Walk(
			context.Background(), "id",
			Params{LogGroups: []string{"/g"}, MaxEntries: &v, TextFilter: "keep"}, framework.ForeignContext{})
		if err != nil {
			t.Fatalf("walk: %v", err)
		}
		if !result.Complete.IsComplete() {
			t.Errorf("asserted INCOMPLETE (%q) though the only entry beyond the bound was filtered out",
				result.Complete.Reason())
		}
	})
}

// TestTheBetweenGroupBoundCellIsAlsoMeasured drives the same question across
// log groups: the budget spent before a later group is read is truncation only
// if that group holds something.
func TestTheBetweenGroupBoundCellIsAlsoMeasured(t *testing.T) {
	t.Run("a later group that holds an entry makes the walk INCOMPLETE", func(t *testing.T) {
		groups := []recordedGroup{
			{LogGroup: "/a", Pages: []recordedPage{{Events: []recordedEvent{ev("1", 1772366774000, "alpha one")}}}},
			{LogGroup: "/b", Pages: []recordedPage{{Events: []recordedEvent{ev("2", 1772366780000, "beta one")}}}},
		}
		v := 1
		result, err := collectorWithFake(newFakeClientFor(t, groups)).Walk(
			context.Background(), "id", Params{LogGroups: []string{"/a", "/b"}, MaxEntries: &v}, framework.ForeignContext{})
		if err != nil {
			t.Fatalf("walk: %v", err)
		}
		if result.Complete.IsComplete() {
			t.Fatal("asserted COMPLETE though a named group was never read and holds entries")
		}
		if !strings.Contains(result.Complete.Reason(), "/b") {
			t.Errorf("the reason %q does not name the group that was left unread", result.Complete.Reason())
		}
	})

	// The same loop, on the between-groups probe. Its own empty-page shape is
	// separate code from the cursor probe's and needs its own cell.
	t.Run("a later group whose FIRST page is empty but whose cursor holds data", func(t *testing.T) {
		groups := []recordedGroup{
			{LogGroup: "/a", Pages: []recordedPage{{Events: []recordedEvent{ev("1", 1772366774000, "alpha one")}}}},
			{LogGroup: "/b", Pages: []recordedPage{
				{NextToken: "b2"},
				{Events: []recordedEvent{ev("2", 1772366780000, "beta one")}},
			}},
		}
		v := 1
		result, err := collectorWithFake(newFakeClientFor(t, groups)).Walk(
			context.Background(), "id", Params{LogGroups: []string{"/a", "/b"}, MaxEntries: &v},
			framework.ForeignContext{})
		if err != nil {
			t.Fatalf("walk: %v", err)
		}
		if result.Complete.IsComplete() {
			t.Fatal("asserted COMPLETE though the unread group's data sat one empty page in; the group probe " +
				"must follow an empty page rather than stopping at it")
		}
		if !strings.Contains(result.Complete.Reason(), "/b") {
			t.Errorf("the reason %q does not name the group that was left unread", result.Complete.Reason())
		}
	})

	t.Run("a later group that is EMPTY leaves the walk COMPLETE", func(t *testing.T) {
		groups := []recordedGroup{
			{LogGroup: "/a", Pages: []recordedPage{{Events: []recordedEvent{ev("1", 1772366774000, "alpha one")}}}},
			{LogGroup: "/b", Pages: []recordedPage{{}}},
		}
		v := 1
		result, err := collectorWithFake(newFakeClientFor(t, groups)).Walk(
			context.Background(), "id", Params{LogGroups: []string{"/a", "/b"}, MaxEntries: &v}, framework.ForeignContext{})
		if err != nil {
			t.Fatalf("walk: %v", err)
		}
		if !result.Complete.IsComplete() {
			t.Errorf("asserted INCOMPLETE (%q) though the unread group holds nothing", result.Complete.Reason())
		}
	})
}
