// SPDX-License-Identifier: Apache-2.0

package correlation

import (
	"strings"
	"testing"
	"time"
)

// badinput_test.go — BAD INPUT ALWAYS ERRORS, and the arms that must NOT error.
//
// THIS IS NEW SURFACE, NOT PARITY, and saying so is the point: the parity
// target's detector has no error return, because it is an unexported function
// inside one package whose only caller is the pipeline that built its inputs.
// An exported API can be handed shapes that caller never could, and each arm
// below is one the target either PANICS on (it dereferences s.ID and
// c.TemplateID without a nil check) or silently mis-attributes (an empty id
// keys a map entry that another empty id then resolves against). None of them
// occurs in well-formed input, so no existing fixture changes.
//
// THE NEGATIVE ARMS AT THE BOTTOM ARE WHAT KEEPS THIS FROM BECOMING A REFUSAL
// MACHINE: a nil resolver, a nil oracle, an empty input and an unset timestamp
// are all MEANINGFUL inputs with documented behaviour, and an implementation
// that refused them would be diverging from parity in the other direction.

func TestFindCorrelations_RefusesMalformedInput(t *testing.T) {
	base := time.Date(2026, 4, 13, 14, 0, 0, 0, time.UTC)
	api := streamFor("api")
	good := templateAt("tpl-good", SeverityError, base, time.Minute)

	for _, tc := range []struct {
		name   string
		in     Input
		needle string
	}{
		{
			name: "a nil template",
			in: Input{
				Templates: []*Template{good, nil},
				Chunks:    []*Chunk{chunkFor(api, good)},
				Streams:   []*Stream{api},
			},
			needle: "Templates[1] is nil",
		},
		{
			name: "a template with no id",
			in: Input{
				Templates: []*Template{good, {Severity: SeverityError, FirstSeen: base, LastSeen: base}},
				Chunks:    []*Chunk{chunkFor(api, good)},
				Streams:   []*Stream{api},
			},
			needle: "Templates[1] has an empty ID",
		},
		{
			name: "a template whose range runs backwards",
			in: Input{
				Templates: []*Template{{
					ID: "tpl-inverted", Severity: SeverityError,
					FirstSeen: base.Add(time.Hour), LastSeen: base,
				}},
				Chunks:  []*Chunk{},
				Streams: []*Stream{api},
			},
			needle: "LastSeen",
		},
		{
			name: "a nil chunk",
			in: Input{
				Templates: []*Template{good},
				Chunks:    []*Chunk{chunkFor(api, good), nil},
				Streams:   []*Stream{api},
			},
			needle: "Chunks[1] is nil",
		},
		{
			name: "a nil stream",
			in: Input{
				Templates: []*Template{good},
				Chunks:    []*Chunk{chunkFor(api, good)},
				Streams:   []*Stream{api, nil},
			},
			needle: "Streams[1] is nil",
		},
		{
			name: "a stream with no id",
			in: Input{
				Templates: []*Template{good},
				Chunks:    []*Chunk{chunkFor(api, good)},
				Streams:   []*Stream{api, {Labels: map[string]string{FieldService: "db"}}},
			},
			needle: "Streams[1] has an empty ID",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			results, err := FindCorrelations(tc.in)
			if err == nil {
				t.Fatalf("expected a refusal, got %d results and no error", len(results))
			}
			if results != nil {
				t.Errorf("a refusal returns no results, got %+v", results)
			}
			if !strings.Contains(err.Error(), tc.needle) {
				t.Errorf("the error does not name the condition: %v (want a mention of %q)", err, tc.needle)
			}
			if !strings.Contains(err.Error(), "correlation:") {
				t.Errorf("the error does not name its package: %v", err)
			}
		})
	}
}

// TestFindCorrelations_AcceptsTheMeaningfulZeroes is the other half, and without
// it the refusals above are consistent with an implementation that refuses
// everything.
func TestFindCorrelations_AcceptsTheMeaningfulZeroes(t *testing.T) {
	base := time.Date(2026, 4, 13, 14, 0, 0, 0, time.UTC)
	api, db := streamFor("api"), streamFor("db")
	a := templateAt("tpl-a", SeverityError, base, time.Minute)
	b := templateAt("tpl-b", SeverityError, base, time.Minute)
	// AN UNSET TIMESTAMP IS NOT MALFORMED: an unknown bound passes through
	// unpadded, which is documented parity (pipeline_correlation.go:323-329).
	unset := &Template{ID: "tpl-unset", Severity: SeverityError}

	for _, tc := range []struct {
		name string
		in   Input
	}{
		{"an empty input", Input{}},
		{"a nil resolver and a nil oracle", Input{
			Templates: []*Template{a, b},
			Chunks:    []*Chunk{chunkFor(api, a), chunkFor(db, b)},
			Streams:   []*Stream{api, db},
		}},
		{"a nil proxy map", Input{
			Templates: []*Template{a, b},
			Chunks:    []*Chunk{chunkFor(api, a), chunkFor(db, b)},
			Streams:   []*Stream{api, db},
			Resolver:  newResolver(map[string]string{"api": "arn:api", "db": "arn:db"}),
			Oracle:    newOracle(),
		}},
		{"a template with unset timestamps", Input{
			Templates: []*Template{a, unset},
			Chunks:    []*Chunk{chunkFor(api, a), chunkFor(db, unset)},
			Streams:   []*Stream{api, db},
		}},
		// HALF A RANGE IS STILL NOT BACKWARDS. This is the case that makes the
		// two IsZero guards in rangeRunsBackwards load-bearing: without them an
		// unset LastSeen precedes any known FirstSeen and every half-known range
		// would be refused.
		{"a template with a known start and an unknown end", Input{
			Templates: []*Template{a, {ID: "tpl-half", Severity: SeverityError, FirstSeen: base}},
			Chunks:    []*Chunk{chunkFor(api, a), {StreamID: db.ID, TemplateID: "tpl-half"}},
			Streams:   []*Stream{api, db},
		}},
		{"a template with an unknown start and a known end", Input{
			Templates: []*Template{a, {ID: "tpl-half2", Severity: SeverityError, LastSeen: base}},
			Chunks:    []*Chunk{chunkFor(api, a), {StreamID: db.ID, TemplateID: "tpl-half2"}},
			Streams:   []*Stream{api, db},
		}},
		{"a stream with no labels at all", Input{
			Templates: []*Template{a, b},
			Chunks:    []*Chunk{chunkFor(api, a), {StreamID: "stream-bare", TemplateID: b.ID}},
			Streams:   []*Stream{api, {ID: "stream-bare"}},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := FindCorrelations(tc.in); err != nil {
				t.Errorf("a meaningful zero was refused: %v", err)
			}
		})
	}
}

// TestFindCorrelations_ValidatesEveryTemplateNotJustTheErrorOnes is the arm that
// stops the validation from being folded into the ERROR filter, where a
// malformed INFO template would pass unnoticed and then reach whatever the
// caller does with the slice next.
func TestFindCorrelations_ValidatesEveryTemplateNotJustTheErrorOnes(t *testing.T) {
	base := time.Date(2026, 4, 13, 14, 0, 0, 0, time.UTC)
	api := streamFor("api")
	good := templateAt("tpl-good", SeverityError, base, time.Minute)
	badInfo := &Template{ID: "", Severity: SeverityInfo}

	_, err := FindCorrelations(Input{
		Templates: []*Template{good, badInfo},
		Chunks:    []*Chunk{chunkFor(api, good)},
		Streams:   []*Stream{api},
	})
	if err == nil {
		t.Fatal("a malformed INFO template must be refused, not filtered away unseen")
	}
	if !strings.Contains(err.Error(), "Templates[1] has an empty ID") {
		t.Errorf("the error does not name the condition: %v", err)
	}
}
