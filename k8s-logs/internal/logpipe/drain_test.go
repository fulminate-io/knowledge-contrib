// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"strings"
	"testing"
	"time"
)

// drain_test.go — clustering, and the three derived properties a one-entry
// fixture cannot reach.

func at(sec int) time.Time { return time.Unix(int64(sec), 0).UTC() }

func entry(msg string, sec int) Entry {
	return Entry{Timestamp: at(sec), Severity: DeriveSeverity(msg), Message: msg, Labels: map[string]string{"a": "1"}}
}

// TestTemplateIDIsTruncatedAndBare pins the id's SHAPE, which is what an
// implementation gets wrong while every type assertion still passes.
func TestTemplateIDIsTruncatedAndBare(t *testing.T) {
	id := TemplateID("Connection refused")
	if len(id) != 32 {
		t.Fatalf("template id is %d characters (%q); the log graph's is 32, a truncated sha256", len(id), id)
	}
	if strings.ContainsAny(id, ":") {
		t.Fatalf("template id %q carries a prefix; a template id is bare", id)
	}
	if TemplateID("Connection refused") != id {
		t.Fatal("template id is not a function of the pattern alone")
	}
	if TemplateID("Connection refuse") == id {
		t.Fatal("two different patterns hash to one template id")
	}
}

// TestTemplateIDMovesWhenThePatternBroadens is the property that makes
// resolving ids at insertion time wrong.
func TestTemplateIDMovesWhenThePatternBroadens(t *testing.T) {
	d := NewDrainEngine(DefaultDrainConfig())
	first := d.AddMessage(entry("Connection to db-1 failed after three retries", 100))
	idAtInsertion := first.ID

	second := d.AddMessage(entry("Connection to db-2 failed after three retries", 101))
	if second != first {
		t.Fatal("the two messages did not cluster together; the fixture no longer exercises a broadening pattern")
	}
	if first.ID == idAtInsertion {
		t.Fatalf("the template id did not move when the pattern broadened to %q; "+
			"an id captured at insertion would then be valid, and it is not", first.Pattern)
	}
	if !strings.Contains(first.Pattern, Wildcard) {
		t.Fatalf("the pattern %q did not broaden to a wildcard", first.Pattern)
	}
	if first.ID != TemplateID(first.Pattern) {
		t.Fatalf("the template id %q is not the hash of its own final pattern", first.ID)
	}
}

// TestTemplateAliasIsRecomputedAfterAMerge is the alias half of the same
// property. A fixture of one entry per template never reaches it.
func TestTemplateAliasIsRecomputedAfterAMerge(t *testing.T) {
	d := NewDrainEngine(DefaultDrainConfig())
	tpl := d.AddMessage(Entry{Timestamp: at(100), Severity: SeverityError,
		Message: "Connection to db-1 failed after 3 retries", Labels: map[string]string{"a": "1"}})
	aliasBeforeMerge := tpl.Alias

	d.AddMessage(Entry{Timestamp: at(101), Severity: SeverityError,
		Message: "Connection to db-2 failed after 9 retries", Labels: map[string]string{"a": "1"}})

	if tpl.Alias == aliasBeforeMerge {
		t.Fatalf("the alias stayed %q after the pattern broadened to %q; "+
			"an alias derived once at materialization would look correct and describe a pattern the template no longer has",
			tpl.Alias, tpl.Pattern)
	}
	if want := "connection-failed-after-retries@err"; tpl.Alias != want {
		t.Fatalf("alias after the merge is %q, want %q (derived from the BROADENED pattern %q)", tpl.Alias, want, tpl.Pattern)
	}
}

// TestTemplateAliasStripsACapitalisedPrefix pins the behavior that surprises a
// reader: the alias is not the message.
func TestTemplateAliasStripsACapitalisedPrefix(t *testing.T) {
	for _, tc := range []struct {
		name     string
		pattern  string
		severity string
		want     string
	}{
		{"capitalised prefix is stripped", "ERROR: connection refused to " + Wildcard, SeverityError, "connection-refused@err"},
		{"a reason prefix is stripped too", "NodeNotReady: Node is not ready", SeverityError, "node-not-ready@err"},
		{"a lowercase prefix survives", "http: TLS handshake error", SeverityError, "http-tls-handshake-error@err"},
		{"no prefix at all", "Node " + Wildcard + " is not ready", SeverityWarn, "node-not-ready@warn"},
		{"an empty severity drops the suffix", "Node is not ready", "", "node-not-ready"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := TemplateAliasFor(&Template{Pattern: tc.pattern, Severity: tc.severity})
			if got != tc.want {
				t.Fatalf("alias of %q at %q is %q, want %q", tc.pattern, tc.severity, got, tc.want)
			}
		})
	}
}

// TestTemplateAliasFallsBackToThePattern covers the cell where every token is a
// wildcard or a stopword, which is where a node would otherwise carry an empty
// SymbolName and be invisible to a reader.
func TestTemplateAliasFallsBackToThePattern(t *testing.T) {
	pattern := Wildcard + " " + Wildcard + " " + Wildcard
	if alias := TemplateAliasFor(&Template{Pattern: pattern, Severity: SeverityInfo}); alias != "" {
		t.Fatalf("an all-wildcard pattern derived the alias %q; it derives none", alias)
	}
	node := templateNode(&Template{ID: "abc", Pattern: pattern, Severity: SeverityInfo})
	if node.SymbolName != pattern {
		t.Fatalf("a template with no derivable alias has SymbolName %q; it must fall back to the pattern %q, "+
			"or the node is unreadable", node.SymbolName, pattern)
	}
}

// TestSeverityRisesToTheMostSevereMember and its unset arm, which is the one
// SeverityIndex cannot express.
func TestSeverityRisesToTheMostSevereMember(t *testing.T) {
	d := NewDrainEngine(DefaultDrainConfig())
	tpl := d.AddMessage(Entry{Timestamp: at(1), Severity: SeverityInfo, Message: "request handled in 12 ms"})
	d.AddMessage(Entry{Timestamp: at(2), Severity: SeverityError, Message: "request handled in 99 ms"})
	if tpl.Severity != SeverityError {
		t.Fatalf("template severity is %q after an ERROR entry joined an INFO cluster; want ERROR", tpl.Severity)
	}

	unset := NewDrainEngine(DefaultDrainConfig())
	u := unset.AddMessage(Entry{Timestamp: at(1), Severity: "", Message: "cache miss for 1111 keys"})
	unset.AddMessage(Entry{Timestamp: at(2), Severity: SeverityTrace, Message: "cache miss for 2222 keys"})
	if u.Severity != SeverityTrace {
		t.Fatalf("a TRACE entry did not raise a template whose severity was unset (got %q); "+
			"an unset level must rank strictly below TRACE or the template keeps an empty level for the whole run", u.Severity)
	}
}

// TestEmptyMessageProducesNoTemplate — the entry class that must be skipped
// rather than attached to an arbitrary cluster.
func TestEmptyMessageProducesNoTemplate(t *testing.T) {
	d := NewDrainEngine(DefaultDrainConfig())
	for _, msg := range []string{"", "   ", "\t\t"} {
		if tpl := d.AddMessage(entry(msg, 1)); tpl != nil {
			t.Fatalf("message %q produced template %q; a message with no tokens belongs to no template", msg, tpl.ID)
		}
	}
	if got := len(d.Templates()); got != 0 {
		t.Fatalf("%d templates were created from messages with no tokens", got)
	}
}

// TestPreProcessReplacesHighCardinalityTokens is what makes clustering
// converge; without it every line with an id is its own template.
func TestPreProcessReplacesHighCardinalityTokens(t *testing.T) {
	for _, tc := range []struct{ name, in, want string }{
		{"uuid", "req 550e8400-e29b-41d4-a716-446655440000 done", "req " + Wildcard + " done"},
		{"ipv4", "dial 10.1.2.3 failed", "dial " + Wildcard + " failed"},
		{"long number", "processed 123456 rows", "processed " + Wildcard + " rows"},
		{"short number survives", "processed 12 rows", "processed 12 rows"},
		{"rfc3339 timestamp", "at 2026-09-07T10:00:00Z ok", "at " + Wildcard + " ok"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := PreProcess(tc.in); got != tc.want {
				t.Fatalf("PreProcess(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
