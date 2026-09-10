// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// drain_classes_test.go — THE INPUT CLASSES THE PARITY TARGET'S OWN SUITE
// COVERS, enumerated from it rather than invented, so a class it observes and
// this module does not is a visible gap rather than an absence nobody counted.
//
// The eleven classes in cmd/knowledge/internal/collector/logs/drain_test.go:
//
//	basic clustering      lines of one shape become one template
//	different patterns    lines of different shapes become different templates
//	preprocessing         high-cardinality tokens are wildcarded first
//	similarity            the scoring function, including its two zero cases
//	merge tokens          broadening a template where it disagrees
//	empty message         a message with no tokens joins no template
//	MAX CLUSTERS          the overflow path past the cluster cap
//	template id           the id's shape and its input
//	first and last seen   a cluster's time bounds widen in both directions
//	example vars          variable values are captured from later matches
//	severity tracking     a cluster rises to its most severe member
//
// Four were already covered here (preprocessing, empty message, template id,
// severity tracking) in drain_test.go. The rest are below.
//
// THE OVERFLOW CLASS IS WHY THIS FILE EXISTS. Past the cluster cap, which
// template an entry is ATTRIBUTED to — and therefore which template its chunk
// is filed under — is decided entirely by handleOverflow and
// findBestGlobalMatch. Both sat at zero coverage, and a panic planted in the
// overflow path left the whole suite green. No entry is dropped there, so it is
// not a cap under the no-caps ruling; it is a correctness path that decides
// where data lands.

// TestDrainBasicClustering — one shape, one template, counted.
func TestDrainBasicClustering(t *testing.T) {
	d := NewDrainEngine(DefaultDrainConfig())
	for i := range 3 {
		d.AddMessage(Entry{
			Timestamp: at(i),
			Severity:  SeverityInfo,
			Message:   fmt.Sprintf("served the request for user alpha in %d ms", i),
		})
	}
	templates := d.Templates()
	if len(templates) != 1 {
		t.Fatalf("three lines of one shape made %d templates, want 1: %v", len(templates), patternList(templates))
	}
	if templates[0].Count != 3 {
		t.Fatalf("the template counts %d entries, want 3", templates[0].Count)
	}
}

// TestDrainDifferentPatterns — the control for the row above: shapes that
// differ do NOT collapse together.
func TestDrainDifferentPatterns(t *testing.T) {
	d := NewDrainEngine(DefaultDrainConfig())
	for _, msg := range []string{
		"served the request for user alpha",
		"database connection pool exhausted after waiting",
		"cache invalidated by a write to the primary",
	} {
		d.AddMessage(Entry{Timestamp: at(1), Severity: SeverityInfo, Message: msg})
	}
	if got := len(d.Templates()); got != 3 {
		t.Fatalf("three unrelated shapes made %d templates, want 3: %v", got, patternList(d.Templates()))
	}
}

// TestDrainSimilarity — the scoring function, including the two inputs that
// score zero for structural reasons rather than for disagreement.
func TestDrainSimilarity(t *testing.T) {
	for _, tc := range []struct {
		name string
		a, b []string
		want float64
	}{
		{"identical", []string{"GET", "/api", "200"}, []string{"GET", "/api", "200"}, 1.0},
		{"one position differs", []string{"GET", "/api", "200"}, []string{"GET", "/api", "404"}, 2.0 / 3.0},
		{"every position differs", []string{"GET", "/api", "200"}, []string{"POST", "/users", "500"}, 0.0},
		{"a wildcard counts as agreement", []string{"GET", Wildcard, "200"}, []string{"GET", "/api", "200"}, 1.0},
		{"different lengths score zero", []string{"GET", "/api"}, []string{"GET", "/api", "200"}, 0.0},
		{"two empties score zero", []string{}, []string{}, 0.0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := similarity(tc.a, tc.b); got != tc.want {
				t.Fatalf("similarity(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// TestDrainMergeTokens — broadening, and the length guard that cannot arise
// from the matching path but must not truncate a pattern if a future caller
// reaches it.
func TestDrainMergeTokens(t *testing.T) {
	got := mergeTokens([]string{"GET", "/api/users", Wildcard, "200"}, []string{"GET", "/api/users", "789", "200"})
	want := []string{"GET", "/api/users", Wildcard, "200"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("mergeTokens = %v, want %v", got, want)
	}

	got = mergeTokens([]string{"GET", "/api", "200"}, []string{"GET", "/api", "404"})
	want = []string{"GET", "/api", Wildcard}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("a disagreeing position did not broaden: %v, want %v", got, want)
	}

	template := []string{"GET", "/api"}
	if got := mergeTokens(template, []string{"GET", "/api", "200"}); len(got) != len(template) {
		t.Fatalf("a length mismatch changed the template to %v; it must be left alone rather than truncated", got)
	}
}

// TestDrainFirstAndLastSeenWidenInBothDirections — an entry EARLIER than the
// cluster's first must move FirstSeen back, which a one-directional update
// would not do.
func TestDrainFirstAndLastSeenWidenInBothDirections(t *testing.T) {
	middle := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	latest := middle.Add(2 * time.Hour)
	earliest := middle.Add(-2 * time.Hour)

	d := NewDrainEngine(DefaultDrainConfig())
	for _, ts := range []time.Time{middle, latest, earliest} {
		d.AddMessage(Entry{Timestamp: ts, Severity: SeverityInfo, Message: "server started on port alpha"})
	}
	templates := d.Templates()
	if len(templates) != 1 {
		t.Fatalf("%d templates, want 1", len(templates))
	}
	if !templates[0].FirstSeen.Equal(earliest) {
		t.Fatalf("FirstSeen is %s, want the earliest entry's %s", templates[0].FirstSeen, earliest)
	}
	if !templates[0].LastSeen.Equal(latest) {
		t.Fatalf("LastSeen is %s, want the latest entry's %s", templates[0].LastSeen, latest)
	}

	// A ZERO timestamp must not drag FirstSeen to the epoch for the whole
	// cluster, which is the failure the guard in updateTimeRange prevents.
	d.AddMessage(Entry{Severity: SeverityInfo, Message: "server started on port beta"})
	if !templates[0].FirstSeen.Equal(earliest) {
		t.Fatalf("an entry with no timestamp moved FirstSeen to %s", templates[0].FirstSeen)
	}
}

// TestDrainExampleVarsAreCapturedFromLaterMatches — the first entry creates the
// template, later ones contribute the values at its wildcard positions.
func TestDrainExampleVarsAreCapturedFromLaterMatches(t *testing.T) {
	d := NewDrainEngine(DefaultDrainConfig())
	var tpl *Template
	// The varying token carries DIGITS so the parse tree treats it as variable
	// and routes all three messages to one leaf. A word that differs in a
	// branching position sends them to three leaves and three templates, which
	// is a different class and is covered by TestDrainDifferentPatterns.
	for _, user := range []string{"id1001", "id2002", "id3003"} {
		tpl = d.AddMessage(Entry{Timestamp: at(1), Severity: SeverityInfo,
			Message: "user " + user + " logged in successfully"})
	}
	if len(tpl.ExampleVars) == 0 {
		t.Fatalf("no example values were captured from the later matches of %q", tpl.Pattern)
	}
	if len(tpl.ExampleVars) > maxExampleVars {
		t.Fatalf("%d example rows, more than the %d this template keeps", len(tpl.ExampleVars), maxExampleVars)
	}
	if !strings.Contains(tpl.Pattern, Wildcard) {
		t.Fatalf("the pattern %q did not broaden, so there was no wildcard position to capture from", tpl.Pattern)
	}
}

// TestDrainOverflowAttributesRatherThanDropping is the class the whole file was
// added for.
//
// Past the cluster cap an entry no longer creates a template. It merges into
// its best global match, and falls back to the newest cluster when nothing
// scores above the threshold. Both arms are driven here, because the fallback
// is what runs for a message that resembles nothing already clustered — which
// is exactly the message most likely to arrive at a saturated engine.
func TestDrainOverflowAttributesRatherThanDropping(t *testing.T) {
	cfg := DefaultDrainConfig()
	cfg.MaxClusters = 5

	d := NewDrainEngine(cfg)
	// Fill the engine with distinct shapes, one per cluster.
	shapes := []string{
		"alpha service reported a healthy status",
		"beta cache evicted an entry from the pool",
		"gamma writer flushed a segment to disk",
		"delta reader opened a snapshot for scanning",
		"epsilon planner rebuilt its routing table",
	}
	for _, msg := range shapes {
		d.AddMessage(Entry{Timestamp: at(1), Severity: SeverityInfo, Message: msg})
	}
	if got := len(d.Templates()); got != cfg.MaxClusters {
		t.Fatalf("%d templates after %d distinct shapes at a cap of %d", got, len(shapes), cfg.MaxClusters)
	}

	t.Run("a similar message merges into its best match", func(t *testing.T) {
		before := len(d.Templates())
		got := d.AddMessage(Entry{Timestamp: at(2), Severity: SeverityInfo,
			Message: "alpha service reported a degraded status"})
		if got == nil {
			t.Fatal("an overflowing entry was DROPPED; past the cap an entry is attributed, never discarded")
		}
		if len(d.Templates()) != before {
			t.Fatalf("the template count moved to %d past the cap of %d", len(d.Templates()), cfg.MaxClusters)
		}
		if !strings.Contains(got.Pattern, "alpha") {
			t.Fatalf("the overflowing entry was attributed to %q, not to the shape it resembles", got.Pattern)
		}
	})

	t.Run("a message resembling nothing takes the fallback", func(t *testing.T) {
		// A DIFFERENT TOKEN COUNT, so it routes to a leaf holding no clusters
		// and findMatchingCluster returns nil; and it resembles no existing
		// cluster, so findBestGlobalMatch returns nil too. That is the fallback
		// arm, and it is the one a saturated engine takes most often.
		before := len(d.Templates())
		got := d.AddMessage(Entry{Timestamp: at(3), Severity: SeverityError, Message: "zeta"})
		if got == nil {
			t.Fatal("an entry resembling nothing was DROPPED at the cap rather than attributed")
		}
		if len(d.Templates()) != before {
			t.Fatalf("the fallback created a template: the count moved to %d", len(d.Templates()))
		}
		if got.Count < 2 {
			t.Fatalf("the fallback template counts %d entries; the overflowing entry was not added to it", got.Count)
		}
	})
}

// TestDrainOverflowKeepsEveryEntryAttributed is the whole-pipeline half: no
// entry is lost at the cap, so every one still reaches a chunk.
func TestDrainOverflowKeepsEveryEntryAttributed(t *testing.T) {
	cfg := DefaultDrainConfig()
	cfg.MaxClusters = 4

	entries := make([]Entry, 0, 20)
	for i := range 20 {
		entries = append(entries, Entry{
			Timestamp: at(i),
			Severity:  SeverityInfo,
			Message:   fmt.Sprintf("shape%d reported an event of its own kind", i),
			Labels:    map[string]string{"container": "api", "container_pod": "api-1"},
		})
	}

	templates, entryTemplateIDs := ProcessEntries(entries, cfg)
	if len(templates) > cfg.MaxClusters {
		t.Fatalf("%d templates past a cap of %d", len(templates), cfg.MaxClusters)
	}
	known := TemplatesByID(templates)
	for i, id := range entryTemplateIDs {
		if id == "" {
			t.Fatalf("entry %d was attributed to no template at all", i)
		}
		if _, ok := known[id]; !ok {
			t.Fatalf("entry %d is attributed to template %q, which the result does not carry", i, id)
		}
	}
}
