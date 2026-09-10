// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"
	"time"
)

// drain_test.go — the clustering rules, the id that moves with the pattern, and
// the four aggregates.

func at(sec int) time.Time { return time.Date(2026, 3, 1, 12, 0, sec, 0, time.UTC) }

// TestTemplateIDIsThirtyTwoHexWithNoPrefix pins the id shape. Sixteen hex
// characters is the same shape at a glance and matches nothing the built-in
// path produced.
func TestTemplateIDIsThirtyTwoHexWithNoPrefix(t *testing.T) {
	id := templateID("served request <*> in <*>")
	if len(id) != 32 {
		t.Errorf("template id %q is %d characters, want 32 (16 bytes of sha256)", id, len(id))
	}
	if strings.ContainsAny(id, ":-") {
		t.Errorf("template id %q carries a prefix; a template id has none", id)
	}
}

// TestOneCharacterPatternChangeMovesTheID is the sensitivity control for the id.
func TestOneCharacterPatternChangeMovesTheID(t *testing.T) {
	if templateID("node ready") == templateID("node reads") {
		t.Error("two different patterns produced the same template id")
	}
}

// TestBroadeningMovesTheIDTheAliasAndTheSymbolName is the merge-recompute
// fixture, and it is the only cell an implementation that derives the alias
// once at cluster creation fails.
func TestBroadeningMovesTheIDTheAliasAndTheSymbolName(t *testing.T) {
	d := NewDrainEngine(DefaultDrainConfig())

	first := d.AddMessage(LogEntry{Timestamp: at(1), Severity: SeverityInfo, Message: "disk pressure detected on node alpha"})
	beforeID, beforeAlias := first.ID, first.Alias

	// The second entry both BROADENS the pattern and RAISES the severity, so
	// all three of the id, the alias and the SymbolName move.
	second := d.AddMessage(LogEntry{Timestamp: at(2), Severity: SeverityCritical, Message: "disk pressure detected on node beta"})
	if first != second {
		t.Fatal("the second entry created a new cluster; the two messages differ in one token and must merge")
	}
	if second.ID == beforeID {
		t.Error("the template id did not move when the pattern broadened")
	}
	if second.Alias == beforeAlias {
		t.Errorf("the alias stayed %q when the pattern broadened and the severity was raised", beforeAlias)
	}
	if !strings.HasSuffix(second.Alias, "@crit") {
		t.Errorf("the alias is %q; the suffix must follow the raised AGGREGATE severity", second.Alias)
	}
	if node := templateNode(second); node.SymbolName != second.Alias {
		t.Errorf("the node symbol name %q did not follow the alias %q", node.SymbolName, second.Alias)
	}
}

// TestTemplateSeverityIsTheMaximumOverTheCluster covers both orders, so an
// implementation taking the first entry's severity and one taking the last's
// each fail one cell.
func TestTemplateSeverityIsTheMaximumOverTheCluster(t *testing.T) {
	for _, tc := range []struct {
		name       string
		severities []string
		want       string
	}{
		{"the most severe arrives FIRST", []string{SeverityError, SeverityInfo, SeverityInfo}, SeverityError},
		{"the most severe arrives LAST", []string{SeverityInfo, SeverityInfo, SeverityError}, SeverityError},
		{"a uniform cluster keeps its level", []string{SeverityWarn, SeverityWarn}, SeverityWarn},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := NewDrainEngine(DefaultDrainConfig())
			var tpl *LogTemplate
			for i, sev := range tc.severities {
				tpl = d.AddMessage(LogEntry{Timestamp: at(i), Severity: sev, Message: "worker task finished ok"})
			}
			if tpl.Severity != tc.want {
				t.Errorf("template severity %q, want %q", tpl.Severity, tc.want)
			}
		})
	}
}

// TestSeverityRankTreatsAnUnknownValueAsLowestPins the aggregate scale, which
// differs from the ordering scale used for filtering: here an unknown value
// ranks BELOW trace, so the first real severity raises it.
func TestSeverityRankTreatsAnUnknownValueAsLowest(t *testing.T) {
	if severityRank("NOT_A_LEVEL") >= severityRank(SeverityTrace) {
		t.Error("an unknown severity ranks at or above TRACE; a template seeded from one would never be raised")
	}
	d := NewDrainEngine(DefaultDrainConfig())
	d.AddMessage(LogEntry{Timestamp: at(1), Severity: "NOT_A_LEVEL", Message: "worker task finished ok"})
	tpl := d.AddMessage(LogEntry{Timestamp: at(2), Severity: SeverityTrace, Message: "worker task finished ok"})
	if tpl.Severity != SeverityTrace {
		t.Errorf("template severity %q, want the raised %q", tpl.Severity, SeverityTrace)
	}
}

// TestFirstSeenIsTheMinimumNotTheFirstArrival covers out-of-order delivery,
// which the recorded fixture also exercises.
func TestFirstSeenIsTheMinimumNotTheFirstArrival(t *testing.T) {
	d := NewDrainEngine(DefaultDrainConfig())
	d.AddMessage(LogEntry{Timestamp: at(30), Severity: SeverityInfo, Message: "worker task finished ok"})
	tpl := d.AddMessage(LogEntry{Timestamp: at(10), Severity: SeverityInfo, Message: "worker task finished ok"})
	if !tpl.FirstSeen.Equal(at(10)) {
		t.Errorf("first seen %s, want the earliest timestamp %s", tpl.FirstSeen, at(10))
	}
	if !tpl.LastSeen.Equal(at(30)) {
		t.Errorf("last seen %s, want the latest timestamp %s", tpl.LastSeen, at(30))
	}
	if tpl.Count != 2 {
		t.Errorf("count %d, want 2", tpl.Count)
	}
}

// TestDifferentTokenBucketsNeverShareATemplate pins the consequence of the
// first-level branching.
func TestDifferentTokenBucketsNeverShareATemplate(t *testing.T) {
	d := NewDrainEngine(DefaultDrainConfig())
	short := d.AddMessage(LogEntry{Timestamp: at(1), Message: "alpha beta gamma"})
	medium := d.AddMessage(LogEntry{Timestamp: at(2), Message: "alpha beta gamma delta"})
	if short == medium {
		t.Error("a three-token and a four-token message joined one template; they are in different buckets")
	}
}

// TestAnEmptyMessageJoinsNoCluster covers the arm chunk assembly depends on.
func TestAnEmptyMessageJoinsNoCluster(t *testing.T) {
	d := NewDrainEngine(DefaultDrainConfig())
	if tpl := d.AddMessage(LogEntry{Timestamp: at(1), Message: "   "}); tpl != nil {
		t.Errorf("a whitespace-only message produced template %q", tpl.ID)
	}
}

// TestMaxClustersIsAHardCap covers the overflow arm, including its fallback: an
// over-cap message with no similar cluster still merges rather than creating a
// template.
func TestMaxClustersIsAHardCap(t *testing.T) {
	cfg := DefaultDrainConfig()
	cfg.MaxClusters = 2
	d := NewDrainEngine(cfg)
	d.AddMessage(LogEntry{Timestamp: at(1), Message: "alpha beta gamma"})
	d.AddMessage(LogEntry{Timestamp: at(2), Message: "delta epsilon zeta eta"})
	d.AddMessage(LogEntry{Timestamp: at(3), Message: "theta iota kappa lambda mu nu xi omicron pi rho"})
	if got := len(d.Templates()); got != 2 {
		t.Errorf("the engine holds %d templates, want the cap of 2", got)
	}
}
