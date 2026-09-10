// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"testing"
	"time"
)

// consolidator_test.go — the two folds, the absorption record, and the group-size
// boundary.

// goPanicEntries is a burst of Go stack frames close enough in time to be one
// crash, and numerous enough to be merged.
func goPanicEntries() []logEntry {
	frames := []string{
		"goroutine 42 [running]:",
		"main.handle(0xc000123456, 0x0)",
		"created by main.serve",
		"/src/app/main.go:88 +0x1a5",
	}
	out := make([]logEntry, 0, len(frames))
	for i, f := range frames {
		out = append(out, testEntry(time.Duration(i)*time.Second, severityError, f, nil))
	}
	return out
}

// TestGoStackFragmentsCollapseToOneCriticalTemplate is the fold itself.
func TestGoStackFragmentsCollapseToOneCriticalTemplate(t *testing.T) {
	raw := clusterOnly(goPanicEntries())
	folded, _ := processEntries(goPanicEntries(), defaultDrainConfig())

	if len(folded) >= len(raw) {
		t.Fatalf("consolidation produced %d templates from %d raw ones, want fewer: %s",
			len(folded), len(raw), patternsOf(folded))
	}
	merged := findByPattern(folded, goStackMergedPattern)
	if merged == nil {
		t.Fatalf("no merged template in %s", patternsOf(folded))
	}
	if merged.Severity != severityCritical {
		t.Errorf("the merged template is %s, want %s", merged.Severity, severityCritical)
	}
}

// TestTheMergedTemplateCarriesAComputedID is the first of the three divergences
// from the knowledge client's own pipeline, whose merged template has no id at
// all and therefore lands under a store-generated one that differs between
// collects.
func TestTheMergedTemplateCarriesAComputedID(t *testing.T) {
	folded, _ := processEntries(goPanicEntries(), defaultDrainConfig())
	merged := findByPattern(folded, goStackMergedPattern)
	if merged == nil {
		t.Fatalf("no merged template: %s", patternsOf(folded))
	}
	if merged.ID == "" {
		t.Fatalf("the merged template has an empty id")
	}
	if merged.ID != templateID(goStackMergedPattern) {
		t.Errorf("the merged id %s is not the hash of its own pattern", merged.ID)
	}

	// The property that empty id costs: reproducibility across collects.
	second, _ := processEntries(goPanicEntries(), defaultDrainConfig())
	again := findByPattern(second, goStackMergedPattern)
	if again == nil || again.ID != merged.ID {
		t.Errorf("a second run of the same fixture produced a different merged id")
	}
}

// TestATwoFragmentGroupPassesThroughUnmerged is the group-size boundary: below
// the minimum nothing is folded and nothing is absorbed, so a service that logs
// the odd stack-shaped line keeps its own templates.
func TestATwoFragmentGroupPassesThroughUnmerged(t *testing.T) {
	entries := goPanicEntries()[:2]
	folded, ids := processEntries(entries, defaultDrainConfig())
	if findByPattern(folded, goStackMergedPattern) != nil {
		t.Errorf("two fragments were merged: %s", patternsOf(folded))
	}
	if len(folded) != 2 {
		t.Errorf("got %d templates from two fragments, want 2: %s", len(folded), patternsOf(folded))
	}
	for i, id := range ids {
		if id == "" {
			t.Errorf("entry %d lost its template", i)
		}
	}
}

// TestConsolidationRecordsWhatEachMergeAbsorbed is the relation the remap rule's
// first part reads. Without it the caller can only guess.
func TestConsolidationRecordsWhatEachMergeAbsorbed(t *testing.T) {
	raw := clusterOnly(goPanicEntries())
	out := runConsolidators(defaultConsolidators(), raw)

	if len(out.Absorbed) != 1 {
		t.Fatalf("the fold recorded %d absorptions, want 1: %v", len(out.Absorbed), out.Absorbed)
	}
	for survivor, fragments := range out.Absorbed {
		if survivor != templateID(goStackMergedPattern) {
			t.Errorf("the absorber is %s, want the merged template", survivor)
		}
		if len(fragments) != len(raw) {
			t.Errorf("the merge absorbed %d fragments, want all %d", len(fragments), len(raw))
		}
	}
}

// TestPythonTracebackFragmentsCollapse covers the second consolidator, including
// its header partition.
func TestPythonTracebackFragmentsCollapse(t *testing.T) {
	lines := []string{
		"Traceback (most recent call last):",
		`  File "/app/handler.py", line 20, in serve`,
		"    raise TimeoutError(deadline)",
		"asyncio.exceptions.TimeoutError",
	}
	entries := make([]logEntry, 0, len(lines))
	for i, l := range lines {
		entries = append(entries, testEntry(time.Duration(i)*time.Second, severityError, l, nil))
	}
	folded, ids := processEntries(entries, defaultDrainConfig())

	var merged *logTemplate
	for _, tpl := range folded {
		if len(tpl.Pattern) > 6 && tpl.Pattern[:6] == "Python" {
			merged = tpl
		}
	}
	if merged == nil {
		t.Fatalf("no Python merge in %s", patternsOf(folded))
	}
	if merged.ID != templateID(merged.Pattern) {
		t.Errorf("the merged Python template's id is not the hash of its pattern")
	}
	for i, id := range ids {
		if id == "" {
			t.Errorf("entry %d lost its template through the Python fold", i)
		}
	}
}

// TestTwoSeparateCrashesFoldToOneTemplateRatherThanTwoNodesUnderOneID is the
// duplicate-id case the fixed merged pattern creates. Two bursts more than the
// grouping window apart are two groups, both minting the same pattern and so the
// same id; emitting both would put two nodes under one id in a single batch.
func TestTwoSeparateCrashesFoldToOneTemplateRatherThanTwoNodesUnderOneID(t *testing.T) {
	// The two bursts carry DIFFERENT frame text, so the clusterer produces two
	// disjoint fragment sets rather than one set spanning both. Reusing the same
	// text would cluster the bursts together and produce a single group, which
	// is the shape that makes this case unreachable.
	bursts := [][]string{
		{
			"goroutine 42 [running]:",
			"main.handleAlpha(0xc000123456, 0x0)",
			"created by main.serveAlpha",
			"/src/alpha/main.go:88 +0x1a5",
		},
		{
			"goroutine 77 [running]:",
			"worker.consumeBeta(0xc000999999, 0x1)",
			"created by worker.startBeta",
			"/src/beta/worker.go:12 +0x2b6",
		},
	}
	var entries []logEntry
	for burst, frames := range bursts {
		for i, f := range frames {
			offset := time.Duration(burst)*10*time.Minute + time.Duration(i)*time.Second
			entries = append(entries, testEntry(offset, severityError, f, nil))
		}
	}
	folded, ids := processEntries(entries, defaultDrainConfig())

	seen := make(map[string]int, len(folded))
	for _, tpl := range folded {
		seen[tpl.ID]++
	}
	for id, n := range seen {
		if n > 1 {
			t.Errorf("template id %s names %d emitted templates", id, n)
		}
	}
	merged := findByPattern(folded, goStackMergedPattern)
	if merged == nil {
		t.Fatalf("no merged template across two bursts: %s", patternsOf(folded))
	}
	// The fold is lossless at the template level: the counts and the range of
	// both bursts survive on the one template. The count is compared against the
	// entries that actually resolved to it rather than against the whole
	// fixture, because not every line of a burst is recognized as a stack frame.
	resolvedToMerged := 0
	for _, id := range ids {
		if id == merged.ID {
			resolvedToMerged++
		}
	}
	if resolvedToMerged < 2*goStackMinGroup {
		t.Fatalf("only %d entries resolved to the folded template; the fixture did not produce two merged bursts",
			resolvedToMerged)
	}
	if merged.Count != resolvedToMerged {
		t.Errorf("the folded template counts %d entries but %d resolved to it", merged.Count, resolvedToMerged)
	}
	if merged.LastSeen.Sub(merged.FirstSeen) < 10*time.Minute {
		t.Errorf("the folded template's range %v does not span both bursts",
			merged.LastSeen.Sub(merged.FirstSeen))
	}
	for i, id := range ids {
		if id == "" {
			t.Errorf("entry %d lost its template across the fold", i)
		}
	}
}

// TestOrdinaryOutputIsNotEatenByTheStackDetector is the fold's own negative
// control: the shapes the detector rejects before its permissive arms.
func TestOrdinaryOutputIsNotEatenByTheStackDetector(t *testing.T) {
	for _, tc := range []struct{ name, msg string }{
		{"json object", `{"level":"info","msg":"main.serve(0x1)"}`},
		{"json array", `[1,2,3]`},
		{"timestamped line", "2026-09-07T12:00:00Z main.serve(0x1)"},
		{"logfmt line", "time=2026-09-07 msg=main.serve(0x1)"},
		{"prose mentioning a file", "the handler in main.go:88 was slow but recovered fine"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if matchesGoStackPattern(tc.msg) {
				t.Errorf("%q was read as a Go stack frame", tc.msg)
			}
		})
	}
	// KNOWN POSITIVE: the detector still finds a real frame in the same run.
	if !matchesGoStackPattern("goroutine 42 [running]:") {
		t.Errorf("the detector missed a real goroutine header")
	}
}

// clusterOnly runs the clusterer without consolidation, for the arms that need
// the pre-fold template set as their contrast.
func clusterOnly(entries []logEntry) []*logTemplate {
	d := newDrainEngine(defaultDrainConfig())
	for _, e := range entries {
		d.addMessage(e)
	}
	return d.templates()
}

// findByPattern returns the template carrying an exact pattern.
func findByPattern(templates []*logTemplate, pattern string) *logTemplate {
	for _, tpl := range templates {
		if tpl.Pattern == pattern {
			return tpl
		}
	}
	return nil
}

// mustTemplate builds a template with a derived id, for the remap fixtures.
func mustTemplate(pattern, severity string, first, last time.Duration) *logTemplate {
	tpl := &logTemplate{
		ID:        templateID(pattern),
		Pattern:   pattern,
		Severity:  severity,
		Count:     1,
		FirstSeen: baseTime.Add(first),
		LastSeen:  baseTime.Add(last),
	}
	tpl.Alias = templateAliasFor(tpl)
	return tpl
}

// templateWithID builds a template whose id is forced, for the arms that need
// two survivors tied on both timestamps so the id tie-break is what decides.
func templateWithID(id string, first, last time.Duration) *logTemplate {
	return &logTemplate{
		ID:        id,
		Pattern:   fmt.Sprintf("pattern for %s", id),
		Severity:  severityError,
		Count:     1,
		FirstSeen: baseTime.Add(first),
		LastSeen:  baseTime.Add(last),
	}
}
