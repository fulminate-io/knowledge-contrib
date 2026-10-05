// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"testing"
	"time"
)

// alias_template_test.go — the TEMPLATE alias deriver, which is a second,
// wholly different deriver from the stream one.
//
// The case cell at the foot is the one that proves they are different: this
// deriver lowercases every token where the stream deriver preserves case, so a
// module that reused the stream rules here emits a wrong SymbolName and a wrong
// alias on every template node, on a graph that validates.

func TestTemplateAliasDerivesFromPatternAndSeverity(t *testing.T) {
	cases := []struct {
		name     string
		pattern  string
		severity string
		want     string
	}{
		{name: "wildcards become word breaks", pattern: "Node <*> is not ready", severity: SeverityWarn, want: "node-not-ready@warn"},
		{name: "a reason prefix is stripped", pattern: "NodeNotReady: Node is not ready", severity: SeverityError, want: "node-not-ready@err"},
		{name: "more than five tokens is cut at five", pattern: "one two three four five six seven", severity: SeverityDebug, want: "one-two-three-four-five@debug"},
		{name: "stopwords are dropped", pattern: "the disk is out of space and on fire", severity: SeverityInfo, want: "disk-out-space-fire@info"},
		{name: "digits survive as tokens", pattern: "worker 7 restarted", severity: SeverityInfo, want: "worker-7-restarted@info"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := TemplateAliasFor(&Template{Pattern: tc.pattern, Severity: tc.severity})
			if got != tc.want {
				t.Fatalf("TemplateAliasFor(%q, %s) = %q, want %q", tc.pattern, tc.severity, got, tc.want)
			}
		})
	}
}

// TestReasonPrefixIsStrippedOnlyWhenItIsOne pairs the strip with its near miss.
// The heuristic is deliberately narrow, and a prefix wrongly stripped renames a
// template silently.
func TestReasonPrefixIsStrippedOnlyWhenItIsOne(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
		want    string
	}{
		{name: "single uppercase all-letter token is stripped", pattern: "NodeNotReady: disk failed", want: "disk-failed"},
		{name: "lowercase prefix is kept", pattern: "nodeNotReady: disk failed", want: "nodenotready-disk-failed"},
		{name: "a prefix with a digit is kept", pattern: "Node2Ready: disk failed", want: "node2ready-disk-failed"},
		// Kept, so all five of its tokens survive the cap: node, not, ready,
		// disk, failed. "not" is not in the twelve-word stopword set.
		{name: "a multi-word prefix is kept", pattern: "Node Not Ready: disk failed", want: "node-not-ready-disk-failed"},
		{name: "a one-character prefix is kept", pattern: "N: disk failed", want: "n-disk-failed"},
		{name: "a colon with no space is not a prefix", pattern: "NodeNotReady:disk failed", want: "nodenotready-disk-failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := TemplateAliasFor(&Template{Pattern: tc.pattern})
			if got != tc.want {
				t.Fatalf("TemplateAliasFor(%q) = %q, want %q", tc.pattern, got, tc.want)
			}
		})
	}
}

// TestTemplateAliasSeveritySuffix covers all six canonical severities, the
// empty one that yields NO '@' at all, and a non-canonical one that yields its
// own lowercased self.
func TestTemplateAliasSeveritySuffix(t *testing.T) {
	cases := []struct{ severity, want string }{
		{SeverityCritical, "disk-full@crit"},
		{SeverityError, "disk-full@err"},
		{SeverityWarn, "disk-full@warn"},
		{SeverityInfo, "disk-full@info"},
		{SeverityDebug, "disk-full@debug"},
		{SeverityTrace, "disk-full@trace"},
		{"", "disk-full"},
		{"NOTICE_FROM_SOMEWHERE", "disk-full@notice_from_somewhere"},
	}
	for _, tc := range cases {
		t.Run(tc.severity, func(t *testing.T) {
			got := TemplateAliasFor(&Template{Pattern: "disk full", Severity: tc.severity})
			if got != tc.want {
				t.Fatalf("TemplateAliasFor(severity=%q) = %q, want %q", tc.severity, got, tc.want)
			}
		})
	}
}

// TestTemplateAliasDerivesNothingFromNothing covers the arm the node builder's
// raw-pattern fallback exists for.
func TestTemplateAliasDerivesNothingFromNothing(t *testing.T) {
	for _, pattern := range []string{"", "<*>", "<*> <*>", "the of on for to in is and or with", "!!! ???"} {
		if got := TemplateAliasFor(&Template{Pattern: pattern, Severity: SeverityInfo}); got != "" {
			t.Fatalf("TemplateAliasFor(%q) = %q, want the empty string so the node builder falls back to the raw pattern", pattern, got)
		}
	}
	if got := TemplateAliasFor(nil); got != "" {
		t.Fatalf("TemplateAliasFor(nil) = %q, want the empty string", got)
	}
}

// TestTemplateAliasLowercasesWhereTheStreamAliasPreservesCase is the cell that
// distinguishes the two derivers. Both are handed the same word.
func TestTemplateAliasLowercasesWhereTheStreamAliasPreservesCase(t *testing.T) {
	template := TemplateAliasFor(&Template{Pattern: "OOMKilled container", Severity: SeverityError})
	if want := "oomkilled-container@err"; template != want {
		t.Fatalf("the template deriver = %q, want %q; it lowercases every token", template, want)
	}
	stream := AliasFor(&Stream{Labels: map[string]string{"reason": "OOMKilled"}})
	if want := "OOMKilled"; stream != want {
		t.Fatalf("the stream deriver = %q, want %q; it preserves case", stream, want)
	}
	if template == stream {
		t.Fatal("the two derivers produced the same string for the same word; they are supposed to disagree about case")
	}
}

// TestTemplateAliasMovesWhenTheClusterMerges is the merge cell no other test
// covers: an INFO entry followed by a CRITICAL entry of the same shape leaves
// ONE template at ONE pattern and ONE id whose alias moved. A module deriving
// the alias once at cluster creation diverges on a SymbolName no id assertion
// can see.
func TestTemplateAliasMovesWhenTheClusterMerges(t *testing.T) {
	base := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	d := NewDrainEngine(DefaultDrainConfig())

	first := d.AddMessage(Entry{Timestamp: base, Severity: SeverityInfo, Message: "disk pressure detected"})
	if first == nil {
		t.Fatal("the first message produced no template")
	}
	beforeID, beforeAlias := first.ID, first.Alias
	if beforeAlias != "disk-pressure-detected@info" {
		t.Fatalf("alias before the merge = %q, want %q", beforeAlias, "disk-pressure-detected@info")
	}

	second := d.AddMessage(Entry{Timestamp: base.Add(time.Second), Severity: SeverityCritical, Message: "disk pressure detected"})
	if second != first {
		t.Fatal("the second message did not merge into the first cluster; this cell needs one template, not two")
	}
	if first.ID != beforeID {
		t.Fatalf("the id moved from %q to %q on a merge that did not change the pattern", beforeID, first.ID)
	}
	if first.Alias == beforeAlias {
		t.Fatalf("the alias stayed %q across a severity raise; it is recomputed on every merge because both its inputs move", beforeAlias)
	}
	if want := "disk-pressure-detected@crit"; first.Alias != want {
		t.Fatalf("alias after the merge = %q, want %q", first.Alias, want)
	}
}
