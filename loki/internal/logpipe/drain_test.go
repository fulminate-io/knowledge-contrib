// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"crypto/sha256"
	"fmt"
	"testing"
	"time"
)

// drain_test.go — clustering, and the template id's documented preimage.

func at(offset time.Duration) time.Time {
	return time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC).Add(offset)
}

func msg(text string, offset time.Duration) Entry {
	return Entry{Timestamp: at(offset), Severity: SeverityInfo, Message: text}
}

// TestTemplateIDIsTheDocumentedPreimage asserts the id against sha256 computed
// HERE from the documented rule — the first 16 bytes of sha256(pattern), hex —
// rather than against TemplateID's own output. An id checked against the
// function that produced it agrees with any rule.
func TestTemplateIDIsTheDocumentedPreimage(t *testing.T) {
	const pattern = "disk pressure detected"
	sum := sha256.Sum256([]byte(pattern))
	want := fmt.Sprintf("%x", sum[:16])
	if got := TemplateID(pattern); got != want {
		t.Fatalf("TemplateID(%q) = %q, want %q", pattern, got, want)
	}
	if len(want) != 32 {
		t.Fatalf("the id is %d hex characters, want 32 (16 bytes)", len(want))
	}
}

func TestDrainClustersMessagesByShape(t *testing.T) {
	t.Run("one entry makes one template", func(t *testing.T) {
		d := NewDrainEngine(DefaultDrainConfig())
		if tpl := d.AddMessage(msg("service checkout started", 0)); tpl == nil {
			t.Fatal("no template")
		}
		if n := len(d.Templates()); n != 1 {
			t.Fatalf("templates = %d, want 1", n)
		}
	})

	t.Run("two entries of the same shape merge", func(t *testing.T) {
		d := NewDrainEngine(DefaultDrainConfig())
		a := d.AddMessage(msg("service checkout started ok", 0))
		b := d.AddMessage(msg("service checkout started fine", time.Second))
		if a != b {
			t.Fatal("two messages of one shape produced two clusters")
		}
		if want := "service checkout started <*>"; a.Pattern != want {
			t.Fatalf("merged pattern = %q, want %q", a.Pattern, want)
		}
		if a.Count != 2 {
			t.Fatalf("count = %d, want 2", a.Count)
		}
	})

	t.Run("two entries of different shapes do not merge", func(t *testing.T) {
		d := NewDrainEngine(DefaultDrainConfig())
		d.AddMessage(msg("service checkout started ok", 0))
		d.AddMessage(msg("cache eviction ran twice", time.Second))
		if n := len(d.Templates()); n != 2 {
			t.Fatalf("templates = %d, want 2", n)
		}
	})

	// THE PREFIX TREE SEPARATES BEFORE SIMILARITY IS EVER SCORED, and this is
	// the cell that surprises: the two messages below agree at three of four
	// positions, which is far above the 0.4 threshold, and they still produce
	// two templates — the tree branches on the first MaxDepth-1 tokens, and
	// they differ at token 1, so they never reach a common leaf to be compared.
	// A reader who knows only the threshold predicts one template here.
	t.Run("similar messages differing inside the tree depth do not merge", func(t *testing.T) {
		d := NewDrainEngine(DefaultDrainConfig())
		a := d.AddMessage(msg("service checkout started ok", 0))
		b := d.AddMessage(msg("service payments started ok", time.Second))
		if a == b {
			t.Fatal("the two messages merged; they differ at token 1, which is inside the tree's branching depth")
		}
		if n := len(d.Templates()); n != 2 {
			t.Fatalf("templates = %d, want 2", n)
		}
	})

	t.Run("an empty message produces no template", func(t *testing.T) {
		d := NewDrainEngine(DefaultDrainConfig())
		if tpl := d.AddMessage(msg("   ", 0)); tpl != nil {
			t.Fatalf("a whitespace-only message produced template %q", tpl.ID)
		}
		if n := len(d.Templates()); n != 0 {
			t.Fatalf("templates = %d, want 0", n)
		}
	})
}

// TestDrainTokenCountBucketsAreDisjoint asserts the first-level branch: two
// messages in different buckets never compare, so they cannot merge however
// similar their words are.
func TestDrainTokenCountBucketsAreDisjoint(t *testing.T) {
	for _, tc := range []struct {
		n    int
		want string
	}{
		{1, "short"}, {3, "short"}, {4, "medium"}, {8, "medium"},
		{9, "long"}, {15, "long"}, {16, "vlong"}, {100, "vlong"},
	} {
		if got := tokenCountBucket(tc.n); got != tc.want {
			t.Fatalf("tokenCountBucket(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}

	// The boundary in the engine: three tokens against four, same words.
	d := NewDrainEngine(DefaultDrainConfig())
	d.AddMessage(msg("a b c", 0))
	d.AddMessage(msg("a b c d", time.Second))
	if n := len(d.Templates()); n != 2 {
		t.Fatalf("templates = %d, want 2; a three-token and a four-token message are in different buckets", n)
	}
}

// TestTemplateIDMovesWhenTheClusterBroadens is the property the carry-forward
// diff rests on, asserted rather than assumed away. It is the OPPOSITE of what
// a stability assertion would say, and asserting stability here would make the
// test wrong rather than the code.
func TestTemplateIDMovesWhenTheClusterBroadens(t *testing.T) {
	d := NewDrainEngine(DefaultDrainConfig())
	tpl := d.AddMessage(msg("service checkout started ok", 0))
	before := tpl.ID
	if before != TemplateID("service checkout started ok") {
		t.Fatalf("the id before the merge is not the id of its own pattern")
	}

	d.AddMessage(msg("service checkout started fine", time.Second))
	if tpl.ID == before {
		t.Fatalf("the id stayed %q after a merge broadened the pattern to %q", before, tpl.Pattern)
	}
	if want := TemplateID("service checkout started <*>"); tpl.ID != want {
		t.Fatalf("the id after the merge = %q, want the id of the broadened pattern %q", tpl.ID, want)
	}
}

// TestArrivalOrderIsAssertedNotAssumed. The same message set in a different
// order can produce a different template set, because a cluster's pattern is
// the product of the merges it has seen so far. This test states what the
// engine actually does rather than assuming order-independence: the two orders
// converge on the same PATTERN here, and the assertion is that they do, so a
// change that broke it is visible.
func TestArrivalOrderIsAssertedNotAssumed(t *testing.T) {
	forward := NewDrainEngine(DefaultDrainConfig())
	forward.AddMessage(msg("service checkout started ok", 0))
	forward.AddMessage(msg("service checkout started fine", time.Second))

	backward := NewDrainEngine(DefaultDrainConfig())
	backward.AddMessage(msg("service checkout started fine", 0))
	backward.AddMessage(msg("service checkout started ok", time.Second))

	f, b := forward.Templates(), backward.Templates()
	if len(f) != 1 || len(b) != 1 {
		t.Fatalf("templates: forward=%d backward=%d, want 1 each", len(f), len(b))
	}
	if f[0].Pattern != b[0].Pattern {
		t.Fatalf("the two arrival orders produced patterns %q and %q", f[0].Pattern, b[0].Pattern)
	}
	if f[0].ID != b[0].ID {
		t.Fatalf("the two arrival orders produced ids %q and %q", f[0].ID, b[0].ID)
	}
}

// TestSeverityRaisesButNeverFalls covers the raise rule on the template.
func TestSeverityRaisesButNeverFalls(t *testing.T) {
	d := NewDrainEngine(DefaultDrainConfig())
	tpl := d.AddMessage(Entry{Timestamp: at(0), Severity: SeverityWarn, Message: "disk pressure detected"})
	d.AddMessage(Entry{Timestamp: at(time.Second), Severity: SeverityError, Message: "disk pressure detected"})
	if tpl.Severity != SeverityError {
		t.Fatalf("severity = %q, want %q after an ERROR entry joined a WARN template", tpl.Severity, SeverityError)
	}
	d.AddMessage(Entry{Timestamp: at(2 * time.Second), Severity: SeverityDebug, Message: "disk pressure detected"})
	if tpl.Severity != SeverityError {
		t.Fatalf("severity fell to %q when a DEBUG entry joined; the raise is one-way", tpl.Severity)
	}
}

// TestPreProcessReplacesEachHighCardinalityClass covers every regex, because
// each one decides pattern text and therefore template identity.
func TestPreProcessReplacesEachHighCardinalityClass(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"uuid", "id 7c9e6679-7425-40de-944b-e07fc1f90ae7 done", "id <*> done"},
		{"rfc3339 timestamp", "at 2026-09-07T12:00:00Z done", "at <*> done"},
		{"clock time", "at 12:00:00 done", "at <*> done"},
		{"epoch millis", "at 1788802640167 done", "at <*> done"},
		{"ipv4", "from 10.0.12.4 done", "from <*> done"},
		// A hex id with no ten-digit run, because the timestamp regex claims a
		// run of ten to thirteen digits and would take it first.
		{"long hex", "sha a1b2c3d4e5f6a7b8c9 done", "sha <*> done"},
		{"long number", "port 65535 done", "port <*> done"},
		{"short number is kept", "worker 7 done", "worker 7 done"},
		{"url becomes a named marker", "get https://example.com/a/very/long/path/here done", "get <url> done"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := PreProcess(tc.in); got != tc.want {
				t.Fatalf("PreProcess(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// The classes below are enumerated from THE PARITY TARGET'S OWN SUITE,
// cmd/knowledge/internal/collector/logs/drain_test.go, rather than from the
// prefill's list. That list named five input classes and this engine has
// eleven; the four it omitted are the cluster cap, the first-and-last-seen
// tracking, the example-variable capture and the two scoring units, and a suite
// shaped by the list had none of them.

// TestSimilarityScoresEachTokenRelation is the scoring function itself, which
// decides every merge. The six cells are the parity suite's own.
func TestSimilarityScoresEachTokenRelation(t *testing.T) {
	cases := []struct {
		name string
		a, b []string
		want float64
	}{
		{"identical", []string{"GET", "/api", "200"}, []string{"GET", "/api", "200"}, 1.0},
		{"one difference", []string{"GET", "/api", "200"}, []string{"GET", "/api", "404"}, 2.0 / 3.0},
		{"all different", []string{"GET", "/api", "200"}, []string{"POST", "/users", "500"}, 0.0},
		{"a wildcard on either side matches", []string{"GET", Wildcard, "200"}, []string{"GET", "/api", "200"}, 1.0},
		{"different lengths never compare", []string{"GET", "/api"}, []string{"GET", "/api", "200"}, 0.0},
		{"two empty slices score zero, not one", []string{}, []string{}, 0.0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := similarity(tc.a, tc.b); got != tc.want {
				t.Fatalf("similarity(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// TestMergeTokensWidensOnlyWhereTheyDisagree covers the merge itself: a position
// the two agree on survives, a position they differ on becomes a wildcard, and a
// wildcard already in the template is not narrowed back by a concrete token.
func TestMergeTokensWidensOnlyWhereTheyDisagree(t *testing.T) {
	cases := []struct {
		name             string
		template, tokens []string
		want             []string
	}{
		{"an existing wildcard is kept", []string{"GET", "/api", Wildcard, "200"}, []string{"GET", "/api", "789", "200"},
			[]string{"GET", "/api", Wildcard, "200"}},
		{"a disagreement becomes a wildcard", []string{"GET", "/api", "404"}, []string{"GET", "/api", "200"},
			[]string{"GET", "/api", Wildcard}},
		{"full agreement changes nothing", []string{"GET", "/api"}, []string{"GET", "/api"}, []string{"GET", "/api"}},
		{"a wildcard in the MESSAGE keeps the template's token", []string{"GET", "/api"}, []string{"GET", Wildcard},
			[]string{"GET", "/api"}},
		{"different lengths return the template untouched", []string{"GET", "/api"}, []string{"GET", "/api", "200"},
			[]string{"GET", "/api"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mergeTokens(tc.template, tc.tokens)
			if len(got) != len(tc.want) {
				t.Fatalf("mergeTokens = %v, want %v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("mergeTokens = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// TestExampleVarsAreCapturedFromSubsequentMessages covers the example capture and
// its cap. The FIRST message creates the template and contributes none, because
// there is nothing yet to differ from.
func TestExampleVarsAreCapturedFromSubsequentMessages(t *testing.T) {
	d := NewDrainEngine(DefaultDrainConfig())
	// Numeric ids so every message walks the same wildcard path in the tree.
	tpl := d.AddMessage(msg("user id1001 logged in successfully", 0))
	if len(tpl.ExampleVars) != 0 {
		t.Fatalf("the first message contributed %v; it creates the template and differs from nothing", tpl.ExampleVars)
	}
	for i, m := range []string{"user id2002 logged in successfully", "user id3003 logged in successfully"} {
		if got := d.AddMessage(msg(m, time.Duration(i+1)*time.Second)); got != tpl {
			t.Fatalf("message %d did not join the first cluster", i)
		}
	}
	if len(tpl.ExampleVars) == 0 {
		t.Fatal("no example variables were captured from the later messages")
	}

	// AND THE CAP HOLDS: past maxExampleVars rows nothing more is retained, so a
	// template of a high-volume shape does not grow without bound.
	for i := range 20 {
		d.AddMessage(msg(fmt.Sprintf("user id%d logged in successfully", 4004+i), time.Duration(i+3)*time.Second))
	}
	if len(tpl.ExampleVars) > maxExampleVars {
		t.Fatalf("example variables = %d rows, want at most %d", len(tpl.ExampleVars), maxExampleVars)
	}
}

// TestFirstSeenAndLastSeenTrackTheExtremesUnderOutOfOrderArrival is the class the
// prefill's list omitted and the parity suite carries.
//
// THE EXPECTATIONS ARE LITERAL TIMES, not the template's own fields, and the
// arrival order is deliberately neither ascending nor descending: a FirstSeen
// that tracked the latest, or a LastSeen frozen at the first message, both
// satisfy an assertion written against whatever the producer stored.
func TestFirstSeenAndLastSeenTrackTheExtremesUnderOutOfOrderArrival(t *testing.T) {
	middle := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	latest := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	earliest := time.Date(2026, 1, 1, 8, 0, 0, 0, time.UTC)

	d := NewDrainEngine(DefaultDrainConfig())
	for _, ts := range []time.Time{middle, latest, earliest} {
		d.AddMessage(Entry{Timestamp: ts, Severity: SeverityInfo, Message: "server started on port 8080"})
	}
	templates := d.Templates()
	if len(templates) != 1 {
		t.Fatalf("templates = %d, want 1", len(templates))
	}
	tpl := templates[0]
	if !tpl.FirstSeen.Equal(earliest) {
		t.Fatalf("FirstSeen = %s, want the EARLIEST entry %s", tpl.FirstSeen, earliest)
	}
	if !tpl.LastSeen.Equal(latest) {
		t.Fatalf("LastSeen = %s, want the LATEST entry %s", tpl.LastSeen, latest)
	}
}

// TestAZeroTimestampMovesNeitherBound covers updateTimeRange's own guard: an
// entry carrying no timestamp must not pull FirstSeen back to the zero time.
func TestAZeroTimestampMovesNeitherBound(t *testing.T) {
	known := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	d := NewDrainEngine(DefaultDrainConfig())
	d.AddMessage(Entry{Timestamp: known, Severity: SeverityInfo, Message: "server started on port 8080"})
	d.AddMessage(Entry{Severity: SeverityInfo, Message: "server started on port 9090"})

	tpl := d.Templates()[0]
	if !tpl.FirstSeen.Equal(known) || !tpl.LastSeen.Equal(known) {
		t.Fatalf("the range moved to %s..%s when a zero-timestamped entry joined; want %s twice",
			tpl.FirstSeen, tpl.LastSeen, known)
	}
}
