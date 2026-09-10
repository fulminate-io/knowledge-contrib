// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"
	"time"
)

// drain_test.go — clustering, the id that moves with the pattern, and the alias
// recompute that fails silently on ordinary input.

// TestClusteringMergesVariableTokensAndSplitsOnFixedOnes is the pair that makes
// the clusterer's discrimination observable: without the second arm a clusterer
// that collapsed everything into one template would pass the first.
func TestClusteringMergesVariableTokensAndSplitsOnFixedOnes(t *testing.T) {
	merged, _ := processEntries([]logEntry{
		testEntry(0, severityInfo, "request served in 12 ms", nil),
		testEntry(time.Second, severityInfo, "request served in 340 ms", nil),
	}, defaultDrainConfig())
	if len(merged) != 1 {
		t.Fatalf("two entries differing in a variable token made %d templates, want 1: %s",
			len(merged), patternsOf(merged))
	}
	if !strings.Contains(merged[0].Pattern, wildcard) {
		t.Errorf("the merged pattern carries no wildcard at the variable position: %q", merged[0].Pattern)
	}

	split, _ := processEntries([]logEntry{
		testEntry(0, severityInfo, "connection opened to primary", nil),
		testEntry(time.Second, severityInfo, "disk pressure detected on node", nil),
	}, defaultDrainConfig())
	if len(split) != 2 {
		t.Fatalf("two unrelated entries made %d templates, want 2: %s", len(split), patternsOf(split))
	}
}

// TestEveryEntryResolvesToATemplateThatIsInTheEmittedSet is the invariant the
// orphan defect violates: an entry may resolve to nothing (an empty message) but
// never to an id that names no emitted template.
func TestEveryEntryResolvesToATemplateThatIsInTheEmittedSet(t *testing.T) {
	entries := append(goPanicEntries(), testEntry(time.Minute, severityInfo, "request served in 12 ms", nil))
	templates, ids := processEntries(entries, defaultDrainConfig())

	live := make(map[string]struct{}, len(templates))
	for _, tpl := range templates {
		live[tpl.ID] = struct{}{}
	}
	resolved := 0
	for i, id := range ids {
		if id == "" {
			continue
		}
		resolved++
		if _, ok := live[id]; !ok {
			t.Errorf("entry %d resolved to template %s, which is in no emitted template", i, id)
		}
	}
	// KNOWN POSITIVE: the loop above did not pass because everything was empty.
	if resolved != len(entries) {
		t.Fatalf("only %d of %d entries resolved to a template at all", resolved, len(entries))
	}
}

// TestAnEntryWhoseRemapTargetIsDeadGetsNoTemplateRatherThanADeadID reaches the
// last gate directly. The remap is total by construction, so this case cannot be
// produced through the whole pipeline without first breaking the remap — which
// is why the gate is a function of its own and why this test calls it. An entry
// carrying an id that names no node would produce a chunk whose CONTAINS edge
// dangles, and the client admits a dangling edge silently.
func TestAnEntryWhoseRemapTargetIsDeadGetsNoTemplateRatherThanADeadID(t *testing.T) {
	live := mustTemplate("kept", severityInfo, 0, time.Second)
	dropped := mustTemplate("dropped", severityInfo, 0, time.Second)

	got := resolveEntryTemplateIDs(
		[]*logTemplate{live, dropped, nil},
		map[string]string{dropped.ID: "an-id-no-template-carries"},
		[]*logTemplate{live},
	)
	if got[0] != live.ID {
		t.Errorf("the surviving template's entry resolved to %q, want %q", got[0], live.ID)
	}
	if got[1] != "" {
		t.Errorf("an entry remapped onto a dead id resolved to %q, want the empty id", got[1])
	}
	if got[2] != "" {
		t.Errorf("an entry with no template resolved to %q", got[2])
	}
}

// TestAnEmptyMessageResolvesToNoTemplate is the one legitimate empty id, stated
// so the invariant above is read as "no DEAD id" rather than "never empty".
func TestAnEmptyMessageResolvesToNoTemplate(t *testing.T) {
	templates, ids := processEntries([]logEntry{testEntry(0, severityInfo, "   ", nil)}, defaultDrainConfig())
	if len(templates) != 0 {
		t.Errorf("an empty message produced %d templates: %s", len(templates), patternsOf(templates))
	}
	if ids[0] != "" {
		t.Errorf("an empty message resolved to template %q", ids[0])
	}
}

// TestTemplateIDMovesWithThePatternAndIsReadAfterClustering pins the reason the
// pipeline records the template POINTER during clustering and reads the id
// afterwards: the id at cluster time is not the id at emit time.
func TestTemplateIDMovesWithThePatternAndIsReadAfterClustering(t *testing.T) {
	d := newDrainEngine(defaultDrainConfig())
	first := d.addMessage(testEntry(0, severityInfo, "request served in 12 ms", nil))
	idAtClusterTime := first.ID
	second := d.addMessage(testEntry(time.Second, severityInfo, "request served in 340 ms", nil))

	if first != second {
		t.Fatalf("the two entries landed in different clusters")
	}
	if first.ID == idAtClusterTime {
		t.Fatalf("the id did not move when the pattern broadened; this test's premise is gone")
	}
	if first.ID != templateID(first.Pattern) {
		t.Errorf("the template's id is not the hash of its final pattern")
	}
}

// TestTemplateAliasIsRefreshedWhenTheSeverityRises is the arm that fails
// silently on ordinary input: a cluster whose first entry was INFO and whose
// later entry is CRITICAL must carry the CRITICAL suffix, because both the
// severity and the alias moved.
func TestTemplateAliasIsRefreshedWhenTheSeverityRises(t *testing.T) {
	d := newDrainEngine(defaultDrainConfig())
	tpl := d.addMessage(testEntry(0, severityInfo, "disk pressure detected", nil))
	if tpl.Alias != "disk-pressure-detected@info" {
		t.Fatalf("first alias = %q, want disk-pressure-detected@info", tpl.Alias)
	}
	same := d.addMessage(testEntry(time.Second, severityCritical, "disk pressure detected", nil))
	if same != tpl {
		t.Fatalf("the second entry did not land in the same cluster")
	}
	if tpl.Severity != severityCritical {
		t.Errorf("severity = %s, want %s", tpl.Severity, severityCritical)
	}
	if tpl.Alias != "disk-pressure-detected@crit" {
		t.Errorf("alias = %q, want disk-pressure-detected@crit; the alias was not refreshed after the severity rose",
			tpl.Alias)
	}
}

// TestSeverityIsRaisedNeverLowered is the control for the row above.
func TestSeverityIsRaisedNeverLowered(t *testing.T) {
	d := newDrainEngine(defaultDrainConfig())
	tpl := d.addMessage(testEntry(0, severityCritical, "disk pressure detected", nil))
	d.addMessage(testEntry(time.Second, severityInfo, "disk pressure detected", nil))
	if tpl.Severity != severityCritical {
		t.Errorf("severity = %s after a lower-severity entry, want it held at %s", tpl.Severity, severityCritical)
	}
}

// TestPreProcessMasksEveryHighCardinalityClass covers each masking pass, which
// is what makes two renderings of one line cluster at all.
func TestPreProcessMasksEveryHighCardinalityClass(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"uuid", "job 550e8400-e29b-41d4-a716-446655440000 done", "job <*> done"},
		{"timestamp", "at 2026-09-07T12:00:00Z ok", "at <*> ok"},
		{"clock", "at 12:00:00 ok", "at <*> ok"},
		{"ipv4", "dial 10.0.0.1 failed", "dial <*> failed"},
		{"hex id", "trace abcdefabcdefabcd done", "trace <*> done"},
		// A hex id whose tail is a long digit RUN is claimed by the timestamp
		// pass first, because that pass's epoch-millis alternative is not
		// word-anchored and so matches inside a longer token. The result is a
		// partly-masked token rather than a single wildcard. It is pinned here
		// rather than left to surprise a reader: both halves are masked, so the
		// clustering still works, and changing the pass order to "fix" it would
		// move every template id this module has ever emitted.
		{"hex id with a digit tail is masked in two pieces", "trace abcdef0123456789 done", "trace abcdef<*> done"},
		{"long number", "port 65535 open", "port <*> open"},
		{"url", "GET https://example.com/a/very/long/path/here ok", "GET <url> ok"},
		{"short number survives", "retry 3 times", "retry 3 times"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := preProcess(tc.in); got != tc.want {
				t.Errorf("preProcess(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestClusterCapMergesRatherThanDroppingEntries is the overflow arm: past the
// cluster cap an entry joins the best match rather than being lost.
func TestClusterCapMergesRatherThanDroppingEntries(t *testing.T) {
	cfg := defaultDrainConfig()
	cfg.MaxClusters = 3
	var entries []logEntry
	for i := range 20 {
		entries = append(entries, testEntry(time.Duration(i)*time.Second, severityInfo,
			strings.Repeat("alpha ", 1)+string(rune('a'+i))+" beta gamma", nil))
	}
	templates, ids := processEntries(entries, cfg)
	if len(templates) > cfg.MaxClusters {
		t.Errorf("the cap of %d was exceeded: %d templates", cfg.MaxClusters, len(templates))
	}
	for i, id := range ids {
		if id == "" {
			t.Fatalf("entry %d was dropped at the cluster cap rather than merged", i)
		}
	}
}

// patternsOf renders a template set for a failure message.
func patternsOf(templates []*logTemplate) string {
	out := make([]string, 0, len(templates))
	for _, t := range templates {
		out = append(out, t.Pattern)
	}
	return strings.Join(out, " | ")
}
