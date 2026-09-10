// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"strings"
	"testing"
	"time"
)

// consolidator_test.go — the pass that changes the template SET, and the
// determinism of the remap it leaves behind.

func goStackEntries() []Entry {
	base := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	labels := map[string]string{"container": "api", "container_pod": "api-1", "namespace": "dev"}
	lines := []string{
		"goroutine 17 gp=0xc000102000 m=0 mp=0x1 [running]:",
		"main.crash(0x14000112000, 0x1)",
		"\t/src/app/main.go:42 +0x1c",
		"created by main.serve in goroutine 1",
	}
	entries := make([]Entry, 0, len(lines))
	for i, l := range lines {
		entries = append(entries, Entry{
			Timestamp: base.Add(time.Duration(i) * time.Second),
			Severity:  SeverityError,
			Message:   l,
			Labels:    labels,
		})
	}
	return entries
}

// TestGoStackFragmentsMergeIntoOneTemplate — the pass changes the SET, which
// changes the ids and therefore the CONTAINS edges.
func TestGoStackFragmentsMergeIntoOneTemplate(t *testing.T) {
	entries := goStackEntries()
	clustered := NewDrainEngine(DefaultDrainConfig())
	for _, e := range entries {
		clustered.AddMessage(e)
	}
	before := len(clustered.Templates())
	if before < minFragmentsToMerge {
		t.Fatalf("the fixture clustered into %d templates; it needs at least %d to reach the merge",
			before, minFragmentsToMerge)
	}

	after, _ := ProcessEntries(entries, DefaultDrainConfig())
	if len(after) >= before {
		t.Fatalf("consolidation left %d templates from %d; a goroutine dump is one event, not %d",
			len(after), before, before)
	}
	found := false
	for _, tpl := range after {
		if tpl.Pattern == "Go runtime crash (goroutine dump)" {
			found = true
			if tpl.Severity != SeverityCritical {
				t.Errorf("the merged crash template is %q, want CRITICAL", tpl.Severity)
			}
			if tpl.ID == "" {
				t.Error("the merged template carries NO ID; it becomes a node, so an empty id is an unaddressable node")
			}
			if tpl.ID != TemplateID(tpl.Pattern) {
				t.Errorf("the merged template's id %q is not the hash of its own pattern", tpl.ID)
			}
			if tpl.Alias == "" {
				t.Error("the merged template carries no alias; its pattern and severity are both new")
			}
		}
	}
	if !found {
		t.Fatalf("no merged crash template was produced; patterns present: %v", patternList(after))
	}
}

// TestConsolidatorRemapIsDeterministic is the DELIBERATE DIVERGENCE from the
// built-in pipeline, whose remap picks its replacement from an arbitrary map
// key and therefore produces different template ids on identical input.
func TestConsolidatorRemapIsDeterministic(t *testing.T) {
	// The fixture must leave SEVERAL survivors, or the remap has only one
	// candidate and picking it arbitrarily is indistinguishable from picking it
	// under a stated order. The crash lines are consolidated away; the ordinary
	// lines survive alongside the merged template.
	base := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	labels := map[string]string{"container": "api", "container_pod": "api-1", "namespace": "dev"}
	entries := goStackEntries()
	for i, msg := range []string{
		"served the health endpoint quickly",
		"reloaded the routing table from disk",
		"opened a pool connection to the store",
	} {
		entries = append(entries, Entry{
			Timestamp: base.Add(time.Duration(10+i) * time.Second),
			Severity:  SeverityInfo,
			Message:   msg,
			Labels:    labels,
		})
	}

	survivors, _ := ProcessEntries(entries, DefaultDrainConfig())
	if len(survivors) < 3 {
		t.Fatalf("consolidation left %d templates; the remap needs several candidates before an arbitrary "+
			"pick is distinguishable from a stated one: %v", len(survivors), patternList(survivors))
	}

	_, first := ProcessEntries(entries, DefaultDrainConfig())
	remapped := 0
	for _, id := range first {
		if id != "" {
			remapped++
		}
	}
	if remapped == 0 {
		t.Fatal("no entry resolved to a template; the fixture no longer exercises the remap")
	}

	for i := range 50 {
		_, again := ProcessEntries(entries, DefaultDrainConfig())
		for j := range first {
			if again[j] != first[j] {
				t.Fatalf("run %d: entry %d resolved to template %q, the first run resolved it to %q. "+
					"A remap that picks its replacement from Go's map iteration order produces different "+
					"template and chunk ids on identical input, which is a carry-forward hazard",
					i, j, again[j], first[j])
			}
		}
	}
}

// TestPythonTracebackMergesOnItsHeaderAlone — the arm that does not need three
// fragments.
func TestPythonTracebackMergesOnItsHeaderAlone(t *testing.T) {
	base := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	group := []*Template{
		{ID: "a", Pattern: "Traceback (most recent call last):", Severity: SeverityError, Count: 1,
			FirstSeen: base, LastSeen: base},
		{ID: "b", Pattern: "  File \"/app/main.py\", line 10, in handler", Severity: SeverityError, Count: 1,
			FirstSeen: base.Add(time.Second), LastSeen: base.Add(time.Second)},
		{ID: "c", Pattern: "asyncio.TimeoutError: timed out", Severity: SeverityError, Count: 1,
			FirstSeen: base.Add(2 * time.Second), LastSeen: base.Add(2 * time.Second)},
	}
	out := RunConsolidators([]Consolidator{&pythonTracebackConsolidator{}}, group)
	if len(out) != 1 {
		t.Fatalf("a traceback of three fragments left %d templates: %v", len(out), patternList(out))
	}
	if !strings.HasPrefix(out[0].Pattern, "Python exception: ") {
		t.Fatalf("the merged template's pattern is %q", out[0].Pattern)
	}
	if out[0].ID == "" || out[0].ID != TemplateID(out[0].Pattern) {
		t.Fatalf("the merged template's id is %q, want the hash of its own pattern", out[0].ID)
	}
	if out[0].Count != 3 {
		t.Fatalf("the merged template counts %d, want the three fragments' total", out[0].Count)
	}
}

// TestOrdinaryLinesAreNotConsolidated is the near-miss control: a log line that
// merely mentions a Go source file is not a stack frame.
func TestOrdinaryLinesAreNotConsolidated(t *testing.T) {
	base := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	labels := map[string]string{"container": "api", "container_pod": "api-1"}
	entries := []Entry{
		{Timestamp: base, Severity: SeverityInfo, Message: "time=2026-09-07T10:00:00Z level=info msg=\"served /a\"", Labels: labels},
		{Timestamp: base.Add(time.Second), Severity: SeverityInfo, Message: "{\"msg\":\"served /b\",\"file\":\"main.go:42\"}", Labels: labels},
		{Timestamp: base.Add(2 * time.Second), Severity: SeverityInfo, Message: "loaded config from handler.go:10 successfully", Labels: labels},
	}
	before := NewDrainEngine(DefaultDrainConfig())
	for _, e := range entries {
		before.AddMessage(e)
	}
	after, _ := ProcessEntries(entries, DefaultDrainConfig())
	if len(after) != len(before.Templates()) {
		t.Fatalf("consolidation merged ordinary log lines: %d templates became %d. Patterns: %v",
			len(before.Templates()), len(after), patternList(after))
	}
}

// TestEachStackFrameRejectionDiscriminates asserts the four rejections one at a
// time. The end-to-end arm above can stay green when one rejection is removed,
// because another still fires on the same line — so each is observed here on a
// line that ONLY it rejects.
func TestEachStackFrameRejectionDiscriminates(t *testing.T) {
	for _, tc := range []struct {
		name string
		line string
	}{
		{"a JSON object", `{"msg":"served /b","file":"main.go:42","offset":"+0x1c"}`},
		{"a JSON array", `["main.go:42 +0x1c"]`},
		{"a timestamped line", "2026-09-07 10:00:00 handler.go:10 +0x1c ready"},
		{"a logfmt line", "time=2026-09-07 msg=\"/src/app/main.go:42 +0x1c\""},
		{"a ts= line", "ts=2026-09-07 msg=\"/src/app/main.go:42 +0x1c\""},
		{"long prose with no Go marker", strings.Repeat("the deployment finished successfully ", 12)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if matchesGoStackPattern(tc.line) {
				t.Fatalf("%q was read as a Go stack frame; a line carrying Go-looking text is not a frame, "+
					"and consolidating it merges ordinary log lines into a crash", tc.line)
			}
		})
	}

	// The known positives, so a blanket rejection would be caught: each of these
	// IS a frame.
	for _, line := range []string{
		"goroutine 17 gp=0xc000102000 m=0 [running]:",
		"\t/src/app/main.go:42 +0x1c",
		"created by main.serve in goroutine 1",
		"rax 0x0",
		"main.crash(0x14000112000, 0x1)",
	} {
		if !matchesGoStackPattern(line) {
			t.Errorf("%q was not read as a Go stack frame; it is one", line)
		}
	}
}

func patternList(templates []*Template) []string {
	out := make([]string, 0, len(templates))
	for _, t := range templates {
		out = append(out, t.Pattern)
	}
	return out
}

// TestThePythonHeaderTruncationHoldsItsShape — the ONE bound in this module
// that truncates anything, guarded so a later edit cannot move it silently.
//
// It is kept rather than retired because the merged pattern FEEDS THE TEMPLATE
// ID: changing where the cut falls moves this module's merged-template ids off
// the shared pipeline's, for a graph that is supposed to be at parity with it.
// That makes the exact cut a value worth asserting rather than a tuning knob.
func TestThePythonHeaderTruncationHoldsItsShape(t *testing.T) {
	base := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	header := "Traceback (most recent call last): " + strings.Repeat("frame ", 40)
	if len(header) <= pythonHeaderLimit {
		t.Fatalf("the fixture header is %d characters, which is inside the %d-character bound; "+
			"it must exceed it to reach the truncation", len(header), pythonHeaderLimit)
	}

	group := []*Template{
		{ID: "a", Pattern: header, Severity: SeverityError, Count: 1, FirstSeen: base, LastSeen: base},
		{ID: "b", Pattern: "  File \"/app/main.py\", line 10, in handler", Severity: SeverityError, Count: 1,
			FirstSeen: base.Add(time.Second), LastSeen: base.Add(time.Second)},
		{ID: "c", Pattern: "asyncio.TimeoutError: timed out", Severity: SeverityError, Count: 1,
			FirstSeen: base.Add(2 * time.Second), LastSeen: base.Add(2 * time.Second)},
	}
	out := RunConsolidators([]Consolidator{&pythonTracebackConsolidator{}}, group)
	if len(out) != 1 {
		t.Fatalf("the group did not merge: %d templates", len(out))
	}

	// The expectation is spelled from LITERALS rather than from the constants,
	// so moving either constant is visible here rather than followed.
	want := "Python exception: " + header[:117] + "..."
	if out[0].Pattern != want {
		t.Fatalf("the merged pattern is %q,\nwant %q", out[0].Pattern, want)
	}
	if len(out[0].Pattern) != len("Python exception: ")+120 {
		t.Fatalf("the merged pattern is %d characters after the prefix, want 120", len(out[0].Pattern)-len("Python exception: "))
	}
	if out[0].ID != TemplateID(want) {
		t.Fatalf("the merged template's id is not the hash of its truncated pattern")
	}

	// THE CONTROL: a header INSIDE the bound is carried whole, so the assertion
	// above is about the truncation rather than about the prefix.
	short := "Traceback (most recent call last): ValueError"
	group[0] = &Template{ID: "a", Pattern: short, Severity: SeverityError, Count: 1, FirstSeen: base, LastSeen: base}
	out = RunConsolidators([]Consolidator{&pythonTracebackConsolidator{}}, group)
	if len(out) != 1 || out[0].Pattern != "Python exception: "+short {
		t.Fatalf("a header inside the bound was not carried whole: %q", out[0].Pattern)
	}
}
