// SPDX-License-Identifier: Apache-2.0

package lokiapi

import (
	"testing"
	"time"

	"github.com/fulminate-io/knowledge-contrib/loki/internal/logpipe"
)

// normalize_test.go — one Loki value becoming one entry, and the severity
// precedence, which is the reverse of the order the checks run in.

func TestEveryStreamLabelIsCopiedVerbatim(t *testing.T) {
	labels := map[string]string{"app": "checkout", "reason": "OOMKilled", "empty": ""}
	e := NormalizeEntry(labels, "1700000000000000000", "hello")
	if len(e.Labels) != len(labels) {
		t.Fatalf("copied %d labels of %d: %v", len(e.Labels), len(labels), e.Labels)
	}
	for k, v := range labels {
		if e.Labels[k] != v {
			t.Fatalf("label %q = %q, want %q", k, e.Labels[k], v)
		}
	}
	// THE COPY IS UNCONDITIONAL AND ADDS NO KEY OF ITS OWN. An empty-valued
	// label survives, and no pipeline key appears — which is what keeps the
	// label set the operator's and keeps every alias arm reachable.
	if _, ok := e.Labels["empty"]; !ok {
		t.Fatal("an empty-valued label was dropped by the copy")
	}

	// The copy is a COPY: mutating the source afterwards must not reach the
	// entry, or two entries of one Loki stream would share a map.
	labels["app"] = "mutated"
	if e.Labels["app"] != "checkout" {
		t.Fatal("the entry's labels alias the caller's map")
	}
}

func TestTimestampIsParsedFromNanoseconds(t *testing.T) {
	e := NormalizeEntry(nil, "1700000000000000123", "hello")
	if want := time.Unix(0, 1700000000000000123); !e.Timestamp.Equal(want) {
		t.Fatalf("timestamp = %s, want %s", e.Timestamp, want)
	}

	// AN UNPARSEABLE TIMESTAMP FALLS BACK TO NOW rather than to the epoch. The
	// epoch would put the entry in a chunk bucket fifty years from every other
	// one; "now" keeps it in the collect it arrived in. It is a value from the
	// far side of a network boundary rather than operator input, so it is
	// tolerated rather than refused.
	before := time.Now()
	e = NormalizeEntry(nil, "not-a-number", "hello")
	if e.Timestamp.Before(before) {
		t.Fatalf("an unparseable timestamp produced %s, which is before the call", e.Timestamp)
	}
}

// TestSeverityPrecedenceIsTheReverseOfTheCheckOrder is the inversion cell.
// The stream label is READ FIRST and WINS LAST.
func TestSeverityPrecedenceIsTheReverseOfTheCheckOrder(t *testing.T) {
	cases := []struct {
		name   string
		labels map[string]string
		line   string
		want   string
	}{
		{
			"a JSON body field beats a disagreeing stream label",
			map[string]string{"level": "info"},
			`{"level":"error","message":"upstream refused"}`,
			logpipe.SeverityError,
		},
		{
			"an embedded plain-text marker beats a disagreeing stream label",
			map[string]string{"level": "info"},
			"ERROR upstream refused",
			logpipe.SeverityError,
		},
		{
			"the stream label stands when the line says nothing",
			map[string]string{"level": "warn"},
			"upstream refused",
			logpipe.SeverityWarn,
		},
		{
			"the seed stands when neither says anything",
			nil,
			"upstream refused",
			logpipe.SeverityInfo,
		},
		{
			"a JSON body with no level keeps the stream label",
			map[string]string{"severity": "warn"},
			`{"message":"upstream refused"}`,
			logpipe.SeverityWarn,
		},
		{
			"a JSON body's own level wins even when its message text carries another marker",
			nil,
			`{"level":"debug","message":"ERROR upstream refused"}`,
			logpipe.SeverityDebug,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := NormalizeEntry(tc.labels, "1700000000000000000", tc.line)
			if e.Severity != tc.want {
				t.Fatalf("severity = %q, want %q", e.Severity, tc.want)
			}
		})
	}
}

// TestSeverityIsReadFromEachOfItsLabelAndJSONKeys covers the two key lists.
func TestSeverityIsReadFromEachOfItsLabelAndJSONKeys(t *testing.T) {
	for _, key := range []string{"level", "severity", "detected_level"} {
		e := NormalizeEntry(map[string]string{key: "warn"}, "0", "plain line")
		if e.Severity != logpipe.SeverityWarn {
			t.Fatalf("the stream label %q was not read: severity = %q", key, e.Severity)
		}
	}
	for _, key := range []string{"level", "severity", "lvl"} {
		e := NormalizeEntry(nil, "0", `{"`+key+`":"warn","message":"m"}`)
		if e.Severity != logpipe.SeverityWarn {
			t.Fatalf("the JSON key %q was not read: severity = %q", key, e.Severity)
		}
	}
	// An empty-valued label is not a severity and must fall through.
	e := NormalizeEntry(map[string]string{"level": ""}, "0", "plain line")
	if e.Severity != logpipe.SeverityInfo {
		t.Fatalf("an empty level label produced severity %q, want the seed", e.Severity)
	}
}

// TestMessageExtractionByLineShape covers the JSON and plain-text arms and the
// field-name list.
func TestMessageExtractionByLineShape(t *testing.T) {
	cases := []struct {
		name, line, want string
	}{
		{"a plain line is its own message", "upstream refused", "upstream refused"},
		{"a JSON object yields its message field", `{"message":"upstream refused"}`, "upstream refused"},
		{"msg is read too", `{"msg":"upstream refused"}`, "upstream refused"},
		{"a nested object is descended", `{"body":{"message":"upstream refused"}}`, "upstream refused"},
		{"a JSON object with no known field keeps its own JSON", `{"a":"b"}`, `{"a":"b"}`},
		{"a JSON array is not structured and is kept whole", `["a","b"]`, `["a","b"]`},
		{"malformed JSON is kept whole rather than dropped", `{"message": `, `{"message": `},
		{"an empty line stays empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := NormalizeEntry(nil, "0", tc.line)
			if e.Message != tc.want {
				t.Fatalf("message = %q, want %q", e.Message, tc.want)
			}
		})
	}
}

// TestMessageFieldPriorityOrder pins the order the field names are tried in.
func TestMessageFieldPriorityOrder(t *testing.T) {
	line := `{"body":"last","log":"fifth","textPayload":"fourth","event":"third","msg":"second","message":"first"}`
	if e := NormalizeEntry(nil, "0", line); e.Message != "first" {
		t.Fatalf("message = %q, want %q; `message` is tried before every other name", e.Message, "first")
	}
	if e := NormalizeEntry(nil, "0", `{"body":"last","log":"fifth","msg":"second"}`); e.Message != "second" {
		t.Fatalf("message = %q, want %q", e.Message, "second")
	}
}

// TestParseSeverityVocabulary covers every spelling the canonical mapper
// accepts, and the unknown value that maps to INFO.
func TestParseSeverityVocabulary(t *testing.T) {
	cases := map[string]string{
		"trace": logpipe.SeverityTrace,
		"DBG":   logpipe.SeverityDebug, "debug": logpipe.SeverityDebug,
		"information": logpipe.SeverityInfo, "NOTICE": logpipe.SeverityInfo,
		"warning": logpipe.SeverityWarn, "WARNI": logpipe.SeverityWarn,
		"err": logpipe.SeverityError, "SEVERE": logpipe.SeverityError, "fatal": logpipe.SeverityError,
		"crit": logpipe.SeverityCritical, "ALERT": logpipe.SeverityCritical, "panic": logpipe.SeverityCritical,
		" WARN ":   logpipe.SeverityWarn,
		"nonsense": logpipe.SeverityInfo,
	}
	for in, want := range cases {
		if got := logpipe.ParseSeverity(in); got != want {
			t.Fatalf("ParseSeverity(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestSeverityAtLeastRanksTheVocabulary covers the filter comparison, including
// the value outside the vocabulary that ranks below everything.
func TestSeverityAtLeastRanksTheVocabulary(t *testing.T) {
	if !logpipe.SeverityAtLeast(logpipe.SeverityError, logpipe.SeverityWarn) {
		t.Fatal("ERROR is at least WARN")
	}
	if logpipe.SeverityAtLeast(logpipe.SeverityDebug, logpipe.SeverityWarn) {
		t.Fatal("DEBUG is not at least WARN")
	}
	if !logpipe.SeverityAtLeast(logpipe.SeverityWarn, logpipe.SeverityWarn) {
		t.Fatal("a severity is at least itself")
	}
	if logpipe.SeverityAtLeast("NOT-A-SEVERITY", logpipe.SeverityDebug) {
		t.Fatal("a value outside the vocabulary must not satisfy a minimum above the lowest rank")
	}
}

// TestAnUnencodableStructuredLineDegradesToTheRawLineNotToNothing pins the
// fallback the marshal branch takes. The branch is unreachable in a correct
// build — a map decoded from JSON re-marshals — so the row is written against
// the FUNCTION rather than through a line that cannot exist, and its point is
// that the degrade carries the payload rather than erasing it.
func TestAnUnencodableStructuredLineDegradesToTheRawLineNotToNothing(t *testing.T) {
	// A channel value cannot be marshaled, so this map stands in for the
	// unreachable state a broken build would produce.
	unencodable := map[string]any{"unencodable": make(chan int)}

	if got := extractMessage(unencodable, "the raw line as it arrived"); got != "the raw line as it arrived" {
		t.Fatalf("extractMessage over an unencodable payload = %q, want the raw line", got)
	}
	// THE CONTROL: an encodable payload with no known field still re-encodes,
	// so the fallback above is the failure branch rather than the only branch.
	if got := extractMessage(map[string]any{"a": "b"}, "unused"); got != `{"a":"b"}` {
		t.Fatalf("extractMessage over an encodable payload = %q, want its own JSON", got)
	}
	// And a NESTED payload falls back to nothing, which is what lets the caller
	// try the next field name.
	if got := extractMessage(unencodable, ""); got != "" {
		t.Fatalf("a nested unencodable payload = %q, want the empty string", got)
	}
}
