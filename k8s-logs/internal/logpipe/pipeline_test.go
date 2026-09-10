// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"strings"
	"testing"
	"time"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// pipeline_test.go — the PARITY FIXTURE, asserted on exact derived values
// rather than on type strings, and the determinism a re-collect depends on.

// parityEntries is the fixture. It is written to reach the three shapes a
// one-entry-per-template fixture cannot: a template built from TWO entries
// whose pattern broadens between them, a message opening with a capitalised
// `Word: ` prefix, and a message whose tokens are all wildcards.
func parityEntries() []Entry {
	base := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	pod1 := map[string]string{"container": "api", "container_pod": "api-1", "namespace": "dev"}
	pod2 := map[string]string{"container": "api", "container_pod": "api-2", "namespace": "dev"}
	return []Entry{
		{Timestamp: base, Severity: SeverityError, Message: "connection to database failed for alpha", Labels: pod1},
		{Timestamp: base.Add(time.Second), Severity: SeverityError, Message: "connection to database failed for beta", Labels: pod1},
		{Timestamp: base.Add(2 * time.Second), Severity: SeverityWarn, Message: "NodeNotReady: Node is not ready", Labels: pod2},
		{Timestamp: base.Add(3 * time.Second), Severity: SeverityInfo, Message: "1111 2222 3333", Labels: pod2},
	}
}

// TestParityFixtureDerivedValues asserts the EXACT ids and aliases, which is
// what makes this a parity assertion rather than a shape assertion.
func TestParityFixtureDerivedValues(t *testing.T) {
	res, err := Build(parityEntries(), Options{ChunkWindow: time.Hour})
	if err != nil {
		t.Fatal(err)
	}

	byAlias := map[string]*Template{}
	for _, tpl := range res.Templates {
		byAlias[tpl.Alias] = tpl
	}

	merged, ok := byAlias["connection-database-failed@err"]
	if !ok {
		t.Fatalf("the merged template's alias is not the one derived from the BROADENED pattern; aliases present: %v",
			aliasList(res.Templates))
	}
	if !strings.Contains(merged.Pattern, Wildcard) {
		t.Fatalf("the merged template's pattern %q did not broaden", merged.Pattern)
	}
	if merged.ID != TemplateID(merged.Pattern) {
		t.Fatalf("the merged template's id %q is not the hash of its final pattern", merged.ID)
	}
	if merged.Count != 2 {
		t.Fatalf("the merged template counts %d entries, want 2", merged.Count)
	}

	if _, ok := byAlias["node-not-ready@warn"]; !ok {
		t.Fatalf("the capitalised `NodeNotReady: ` prefix was not stripped from the alias; aliases present: %v",
			aliasList(res.Templates))
	}

	// Every token of the third message is preprocessed to a wildcard, so no
	// alias derives and the node falls back to the pattern.
	wildcardOnly := false
	for _, n := range res.Nodes {
		if n.Type == NodeLogTemplate && n.SymbolName == Wildcard+" "+Wildcard+" "+Wildcard {
			wildcardOnly = true
			if _, hasAlias := n.Metadata["alias"]; hasAlias {
				t.Error("an all-wildcard template carries an alias; none derives from it")
			}
		}
	}
	if !wildcardOnly {
		t.Fatalf("no template fell back to its pattern for a SymbolName; symbol names present: %v", symbolNames(res.Nodes))
	}

	// Stream ids and label node ids, exactly.
	if len(res.Streams) != 2 {
		t.Fatalf("%d streams for two pods, want 2", len(res.Streams))
	}
	for _, s := range res.Streams {
		if len(s.ID) != 64 {
			t.Errorf("stream id %q is not the full 64-hex hash", s.ID)
		}
		if s.Fingerprint == "" {
			t.Errorf("stream %s carries no fingerprint", s.ID)
		}
	}
	for _, want := range []string{
		"log-label:namespace=dev", "log-label:container=api",
		"log-label:container_pod=api-1", "log-label:container_pod=api-2",
	} {
		if !hasLabelNode(res.Nodes, want) {
			t.Errorf("the graph carries no label node %q", want)
		}
	}

	for _, c := range res.Chunks {
		if !strings.HasPrefix(c.ID, "log-chunk:") {
			t.Errorf("chunk id %q carries no prefix", c.ID)
		}
		if c.ID != ChunkID(c.StreamID, c.TemplateID, WindowStart(c.StartTime, time.Hour)) {
			t.Errorf("chunk id %q is not the hash of its own (stream, template, window)", c.ID)
		}
	}
}

// TestStreamAliasDiscriminatesTwoPods is the requirement no natural key naming
// satisfies. It is the reason the emitted pod key is spelled the way it is.
func TestStreamAliasDiscriminatesTwoPods(t *testing.T) {
	res, err := Build(parityEntries(), Options{ChunkWindow: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, n := range res.Nodes {
		if n.Type != NodeLogStream {
			continue
		}
		if n.SymbolName == "" {
			t.Fatalf("stream %s carries no readable name", n.ID)
		}
		if prior, dup := names[n.SymbolName]; dup {
			t.Fatalf("two streams share the readable name %q (%s and %s); "+
				"the emitted label key names must put a discriminating key in the two lowest-sorting positions, "+
				"because a custom graph has no query-time de-collision layer",
				n.SymbolName, prior, n.ID)
		}
		names[n.SymbolName] = n.ID
	}
	if len(names) != 2 {
		t.Fatalf("%d distinct stream names for two pods", len(names))
	}
}

// TestStreamAliasUsesTheGenericArm — the shape, so a reviewer can see the
// provider-shaped arms were not reached.
func TestStreamAliasUsesTheGenericArm(t *testing.T) {
	s := &Stream{Labels: map[string]string{"container": "api", "container_pod": "api-1", "namespace": "dev"}}
	if got, want := AliasFor(s), "container=api.container_pod=api-1"; got != want {
		t.Fatalf("stream alias is %q, want the generic `<key>=<value>.<key>=<value>` form %q", got, want)
	}
	if got := AliasFor(&Stream{}); got != "" {
		t.Fatalf("an empty label set derived the alias %q", got)
	}
	if got, want := AliasFor(&Stream{Labels: map[string]string{"a": "", "b": "2", "c": "3"}}), "b=2.c=3"; got != want {
		t.Fatalf("an empty label value was not skipped: %q, want %q", got, want)
	}
}

// TestReCollectOfUnchangedInputReproducesEveryID is the carry-forward property.
func TestReCollectOfUnchangedInputReproducesEveryID(t *testing.T) {
	first, err := Build(parityEntries(), Options{ChunkWindow: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	for i := range 25 {
		again, err := Build(parityEntries(), Options{ChunkWindow: time.Hour})
		if err != nil {
			t.Fatal(err)
		}
		if len(again.Nodes) != len(first.Nodes) || len(again.Edges) != len(first.Edges) {
			t.Fatalf("run %d emitted %d nodes and %d edges; the first emitted %d and %d",
				i, len(again.Nodes), len(again.Edges), len(first.Nodes), len(first.Edges))
		}
		for j := range first.Nodes {
			if again.Nodes[j].ID != first.Nodes[j].ID {
				t.Fatalf("run %d: node %d is %s, the first run had %s; a moving id breaks carry-forward "+
					"and makes the re-collect's deletion set wrong", i, j, again.Nodes[j].ID, first.Nodes[j].ID)
			}
		}
		for j := range first.Edges {
			if again.Edges[j].FromID != first.Edges[j].FromID || again.Edges[j].ToID != first.Edges[j].ToID {
				t.Fatalf("run %d: edge %d moved", i, j)
			}
		}
	}
}

// TestADifferentWindowIsAParameterNotAnIdentity — the same source read over a
// different chunk window keeps its stream and template ids.
func TestADifferentWindowIsAParameterNotAnIdentity(t *testing.T) {
	wide, err := Build(parityEntries(), Options{ChunkWindow: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	narrow, err := Build(parityEntries(), Options{ChunkWindow: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if idsOfType(wide, NodeLogStream) != idsOfType(narrow, NodeLogStream) {
		t.Fatal("the stream ids changed with the chunk window; a stream's identity is its labels")
	}
	if idsOfType(wide, NodeLogTemplate) != idsOfType(narrow, NodeLogTemplate) {
		t.Fatal("the template ids changed with the chunk window; a template's identity is its pattern")
	}
}

// TestValidateEntriesRefusesAnUnlabelledEntry — bad input errors.
func TestValidateEntriesRefusesAnUnlabelledEntry(t *testing.T) {
	if err := ValidateEntries([]Entry{{Message: "x"}}); err == nil {
		t.Fatal("an entry with no labels was accepted; it identifies no log source")
	}
	if err := ValidateEntries(parityEntries()); err != nil {
		t.Fatalf("the parity fixture was refused: %v", err)
	}
}

func aliasList(templates []*Template) []string {
	out := make([]string, 0, len(templates))
	for _, t := range templates {
		out = append(out, t.Alias)
	}
	return out
}

func symbolNames(nodes []framework.Node) []string {
	out := make([]string, 0)
	for _, n := range nodes {
		if n.Type == NodeLogTemplate {
			out = append(out, n.SymbolName)
		}
	}
	return out
}

func idsOfType(res Result, nodeType string) string {
	out := make([]string, 0)
	for _, n := range res.Nodes {
		if n.Type == nodeType {
			out = append(out, n.ID)
		}
	}
	return strings.Join(out, ",")
}
