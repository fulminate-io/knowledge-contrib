// SPDX-License-Identifier: Apache-2.0

package main

import "testing"

// preprocess_test.go — the masking and bucketing cells. Every id downstream is
// a hash over this stage's output, so each rule gets a cell of its own.

// TestPreProcessMasksEachHighCardinalityShape covers one cell per masking rule,
// plus the ordering cell the rules' interaction depends on.
func TestPreProcessMasksEachHighCardinalityShape(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"uuid", "request 3f2504e0-4f89-11d3-9a0c-0305e82c3301 done", "request <*> done"},
		{"rfc3339 timestamp", "at 2026-03-01T12:06:14Z ok", "at <*> ok"},
		{"clock timestamp", "at 12:06:14 ok", "at <*> ok"},
		{"ipv4", "peer 10.0.0.5 closed", "peer <*> closed"},
		{"hex run", "trace abcdef01ab23cd45ef67 open", "trace <*> open"},
		{"bare long number", "took 1234 units", "took <*> units"},
		{"short number is NOT masked", "took 123 units", "took 123 units"},
		{"url", "fetching https://example.com/a/very/long/path ok", "fetching <url> ok"},
		{"epoch is claimed by the timestamp rule", "at 1772366774000 ok", "at <*> ok"},
		{
			// THE ORDERING CELL, and the one place the order is OBSERVABLE in
			// the output. The timestamp rule's bare-epoch alternative carries
			// no word boundary, so it matches a ten-digit run INSIDE a longer
			// hex identifier and claims it before the hex rule is reached: the
			// identifier below masks to a wildcard followed by its surviving
			// tail, not to a single wildcard. Running the hex rule first would
			// yield "trace <*> open" instead, and every template containing a
			// digit-heavy identifier would get a different id.
			"a digit run inside a hex id is claimed by the timestamp rule",
			"trace 0123456789abcdef0 open",
			"trace <*>abcdef0 open",
		},
		{
			// The URL rule runs LAST, over already-masked text, so a URL whose
			// path carried a long digit run still matches — its remaining tail
			// is over the twenty-character minimum. A URL rule placed first
			// would produce the same "<url>" here, so this cell pins the result
			// rather than the order.
			"a url survives the masking of digits inside it",
			"see https://example.com/build/1772366774000/log",
			"see <url>",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := PreProcess(tc.in); got != tc.want {
				t.Errorf("PreProcess(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestTokenCountBucketBoundaries pins all four buckets at their boundaries. Two
// messages in different buckets can never share a template, so a boundary moved
// by one splits or merges templates silently.
func TestTokenCountBucketBoundaries(t *testing.T) {
	for _, tc := range []struct {
		n    int
		want string
	}{
		{1, "short"}, {3, "short"}, {4, "medium"}, {8, "medium"},
		{9, "long"}, {15, "long"}, {16, "vlong"}, {100, "vlong"},
	} {
		if got := tokenCountBucket(tc.n); got != tc.want {
			t.Errorf("tokenCountBucket(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

// TestIsWildcardCountsDigitBearingTokens pins the rule that a token containing
// a digit branches as variable. Without it the parse tree would branch once per
// distinct short number and split one template per value.
func TestIsWildcardCountsDigitBearingTokens(t *testing.T) {
	for _, tc := range []struct {
		token string
		want  bool
	}{
		{Wildcard, true},
		{"worker-7", true},
		{"7", true},
		{"worker", false},
		{"", false},
	} {
		if got := isWildcard(tc.token); got != tc.want {
			t.Errorf("isWildcard(%q) = %v, want %v", tc.token, got, tc.want)
		}
	}
}
