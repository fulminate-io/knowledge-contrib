// SPDX-License-Identifier: Apache-2.0

package lokiapi

import "testing"

// logql_test.go — the query one collect sends.

func TestBuildLogQLSelectorSources(t *testing.T) {
	cases := []struct {
		name string
		q    Query
		want string
	}{
		{
			"a verbatim selector is used as written",
			Query{Selector: `{app="checkout", env="prod"}`},
			`{app="checkout", env="prod"}`,
		},
		{
			"a verbatim selector wins over the structured fields",
			Query{Selector: `{app="checkout"}`, Source: "prod", FieldFilters: map[string]string{"pod": "p1"}},
			`{app="checkout"}`,
		},
		{
			"the source becomes a namespace matcher",
			Query{Source: "prod"},
			`{namespace="prod"}`,
		},
		{
			"field filters are mapped to this provider's own label names",
			Query{FieldFilters: map[string]string{"service": "checkout", "host": "n1"}},
			`{node="n1", service_name="checkout"}`,
		},
		{
			"an unmapped field passes through, because a Loki's labels are its operator's",
			Query{FieldFilters: map[string]string{"tier": "gold"}},
			`{tier="gold"}`,
		},
		{
			"nothing to select falls back to every namespaced stream",
			Query{},
			`{namespace=~".+"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := BuildLogQL(tc.q); got != tc.want {
				t.Fatalf("BuildLogQL = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestMatchersAreSortedForDeterminism. The matchers come out of a Go map, so
// without the sort two collects over identical input would send different query
// strings. Fifty runs over a three-matcher set is what makes the failure
// certain rather than occasional.
func TestMatchersAreSortedForDeterminism(t *testing.T) {
	q := Query{FieldFilters: map[string]string{"pod": "p1", "container": "c1", "namespace": "n1"}}
	want := `{container="c1", namespace="n1", pod="p1"}`
	for i := range 50 {
		if got := BuildLogQL(q); got != want {
			t.Fatalf("run %d: BuildLogQL = %q, want %q", i, got, want)
		}
	}
}

// TestTheLevelFilterIsExcludedFromTheSelector. Severity is derived from the
// line BODY on this side, so selecting on a `level` LABEL would drop every
// entry whose level is in its body — which is most of them.
func TestTheLevelFilterIsExcludedFromTheSelector(t *testing.T) {
	got := BuildLogQL(Query{FieldFilters: map[string]string{"level": "error", "pod": "p1"}})
	if want := `{pod="p1"}`; got != want {
		t.Fatalf("BuildLogQL = %q, want %q; the level filter must not reach the selector", got, want)
	}
	// The control: with only a level filter there is nothing to select, so the
	// query falls back rather than sending `{}`, which Loki refuses.
	if got := BuildLogQL(Query{FieldFilters: map[string]string{"level": "error"}}); got != `{namespace=~".+"}` {
		t.Fatalf("BuildLogQL with only a level filter = %q, want the fallback selector", got)
	}
}

func TestTextFilterAndRawQueryAreAppended(t *testing.T) {
	cases := []struct {
		name string
		q    Query
		want string
	}{
		{"a text filter becomes a line filter", Query{Source: "prod", TextFilter: "timeout"}, `{namespace="prod"} |= "timeout"`},
		{"a text filter is quoted, so a quote inside it cannot escape", Query{Source: "prod", TextFilter: `say "hi"`}, `{namespace="prod"} |= "say \"hi\""`},
		{"a raw query is appended verbatim", Query{Source: "prod", RawQuery: `| json | line_format "{{.msg}}"`}, `{namespace="prod"} | json | line_format "{{.msg}}"`},
		{"both are appended in order", Query{Source: "prod", TextFilter: "timeout", RawQuery: "| json"}, `{namespace="prod"} |= "timeout" | json`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := BuildLogQL(tc.q); got != tc.want {
				t.Fatalf("BuildLogQL = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestStructuredFilterValuesAreSanitized covers the injection arm. The filters
// reach this collector from a tool call, so they are untrusted in exactly the
// way a query parameter is.
func TestStructuredFilterValuesAreSanitized(t *testing.T) {
	got := BuildLogQL(Query{FieldFilters: map[string]string{`pod"} | drop __error__ | {x="`: `p1"} or {y="`}})
	// Both the name and the value lose every character that could close the
	// matcher or open a new stage. The VALUE sanitizer keeps spaces and '=',
	// which are harmless inside the quoted matcher the value is rendered into,
	// so the surviving text reads as prose rather than as LogQL.
	if want := `{poddrop__error__x="p1 or y="}`; got != want {
		t.Fatalf("BuildLogQL = %q, want %q", got, want)
	}
	// The assertion that matters, stated directly rather than left implicit in
	// the string above: nothing that could terminate the matcher or open a
	// pipeline stage survives on either side.
	for _, dangerous := range []string{`"}`, "|", "{", "}"} {
		if idx := indexOfSubstring(got[1:len(got)-1], dangerous); idx >= 0 {
			t.Fatalf("the built selector body carries %q at %d: %q", dangerous, idx, got)
		}
	}
}

// TestAFieldNameThatSanitizesToNothingIsDropped covers the empty-name arm,
// which would otherwise emit a matcher with no label.
func TestAFieldNameThatSanitizesToNothingIsDropped(t *testing.T) {
	got := BuildLogQL(Query{FieldFilters: map[string]string{`{}"'`: "v", "pod": "p1"}})
	if want := `{pod="p1"}`; got != want {
		t.Fatalf("BuildLogQL = %q, want %q", got, want)
	}
}

// TestASelectorOfOnlyWhitespaceFallsBack covers the trim: an operator who sent
// a blank selector meant to send none.
func TestASelectorOfOnlyWhitespaceFallsBack(t *testing.T) {
	if got := BuildLogQL(Query{Selector: "   "}); got != `{namespace=~".+"}` {
		t.Fatalf("BuildLogQL over a blank selector = %q, want the fallback", got)
	}
}

// indexOfSubstring is a local substring search, kept here so the assertion
// above reads as one expression.
func indexOfSubstring(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
