// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strconv"
	"testing"
)

// defaults_drain_test.go — the CLUSTERING defaults, each straddled by an input
// that crosses it. They decide which entries share a template and therefore
// every template id in the graph. The rest of the production defaults are in
// defaults_test.go.

// TestDrainSimilarityThresholdIsObserved straddles the similarity threshold with
// a pair whose score sits between the production value and one step above it.
func TestDrainSimilarityThresholdIsObserved(t *testing.T) {
	// Ten tokens, five matching: a similarity of exactly 0.5, which merges
	// under the production threshold of 0.4 and would not under 0.5.
	a := "alpha bravo charlie delta echo foxtrot golf hotel india juliet"
	b := "alpha bravo charlie delta echo sierra tango uniform victor whiskey"
	templates, _ := processEntries([]LogEntry{entryAt(0, a), entryAt(1, b)}, DefaultDrainConfig())
	if len(templates) != 1 {
		t.Errorf("two messages at similarity 0.5 produced %d templates, want 1: the production threshold "+
			"admits them", len(templates))
	}
}

// TestDrainMaxDepthIsObserved straddles the parse-tree depth. Branching covers
// the first MaxDepth-1 tokens, so a pair differing at token MaxDepth meets at
// one leaf and merges; one more level of depth separates them.
func TestDrainMaxDepthIsObserved(t *testing.T) {
	a := "alpha bravo charlie delta echo foxtrot golf hotel"
	b := "alpha bravo charlie sierra echo foxtrot golf hotel"
	templates, _ := processEntries([]LogEntry{entryAt(0, a), entryAt(1, b)}, DefaultDrainConfig())
	if len(templates) != 1 {
		t.Errorf("two messages differing at token %d produced %d templates, want 1: branching covers the "+
			"first %d tokens", builtinMaxDepth, len(templates), builtinMaxDepth-1)
	}
}

// letterWord renders i as a LETTER-ONLY word.
//
// The digits matter: a token containing one is treated as variable and branches
// onto the wildcard child, so a fixture built from numbered words fills a parse
// node with ONE child rather than many and cannot reach a child cap at all.
func letterWord(i int) string {
	out := []byte{}
	for {
		out = append([]byte{byte('a' + i%26)}, out...)
		i /= 26
		if i == 0 {
			return string(out)
		}
		i--
	}
}

// TestDrainMaxChildrenIsObserved straddles the per-node child cap. Once a node
// is at the cap every further key collapses onto the wildcard child, so two
// messages with DIFFERENT leading tokens meet at one leaf and merge; one more
// child slot keeps them apart.
func TestDrainMaxChildrenIsObserved(t *testing.T) {
	entries := make([]LogEntry, 0, builtinMaxChildren+2)
	// Fill the bucket node to exactly the built-in cap in distinct first
	// tokens, each message dissimilar from the others so none merges.
	for i := range builtinMaxChildren {
		word := letterWord(i)
		entries = append(entries, entryAt(i, word+" "+word+"xx "+word+"yy "+word+"zz "+word+"ww"))
	}
	// Two further messages with different leading tokens, identical elsewhere.
	entries = append(entries,
		entryAt(builtinMaxChildren, "yankee common common common common"),
		entryAt(builtinMaxChildren+1, "zulu common common common common"))

	templates, _ := processEntries(entries, DefaultDrainConfig())
	byPattern := map[string]int{}
	for _, tpl := range templates {
		byPattern[tpl.Pattern]++
	}
	if byPattern["<*> common common common common"] != 1 {
		t.Errorf("the two over-cap messages did not merge into one wildcard-led template; patterns: %v",
			patternList(templates))
	}
}

// TestDrainMaxClustersIsObserved straddles the cluster cap: at the cap a further
// dissimilar message merges into its best global match instead of creating a
// template, so the count stops rising.
func TestDrainMaxClustersIsObserved(t *testing.T) {
	entries := make([]LogEntry, 0, builtinMaxClusters+1)
	for i := range builtinMaxClusters + 1 {
		word := "cw" + strconv.Itoa(i)
		entries = append(entries, entryAt(i, word+" "+word+"a "+word+"b "+word+"c "+word+"d"))
	}
	templates, _ := processEntries(entries, DefaultDrainConfig())
	if len(templates) != builtinMaxClusters {
		t.Errorf("%d dissimilar messages produced %d templates, want the cap of %d",
			builtinMaxClusters+1, len(templates), builtinMaxClusters)
	}
}

// TestMaxExampleVarsIsObserved pins how many variable rows a template keeps.
// The rows are what the consolidators classify a fragment on, so a template that
// keeps too few can fail to be recognized.
func TestMaxExampleVarsIsObserved(t *testing.T) {
	d := NewDrainEngine(DefaultDrainConfig())
	var tpl *LogTemplate
	for i := range builtinMaxExampleVars + 3 {
		tpl = d.AddMessage(entryAt(i, "worker finished task ident"+strconv.Itoa(i)+"x"))
	}
	if tpl == nil {
		t.Fatal("no template")
	}
	if len(tpl.ExampleVars) != builtinMaxExampleVars {
		t.Errorf("the template kept %d example rows, want the cap of %d",
			len(tpl.ExampleVars), builtinMaxExampleVars)
	}
}
