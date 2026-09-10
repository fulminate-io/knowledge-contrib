// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/common/correlation"
)

// correlate_test.go — the correlation half, end to end and at its seams.
//
// THE ROUND TRIP IS THE LOAD-BEARING ARM and it runs through a spawned child, so
// what it observes is a collect: the envelope decoded by the framework, the
// declared block handed to the walk as an argument, this collect's own templates
// and streams fed to the common detector, the declared edges answering the
// confirmation, and the common module rendering the edge. Nothing in that path
// is substituted except the CloudWatch client, which serves recorded pages.

// TestAConfirmedCorrelationRidesTheEnvelope is the production route for
// CORRELATES_WITH, with the two controls that make its green mean something.
func TestAConfirmedCorrelationRidesTheEnvelope(t *testing.T) {
	params := map[string]any{"log_groups": []any{correlationGroupA, correlationGroupB}}

	t.Run("declared nodes AND the edge between them produce a confirmed edge", func(t *testing.T) {
		child := dialStdioProvider(t, modeCorrelate)
		envelope := callForEnvelope(t, child, map[string]any{
			"id": "instance-1", "params": params, "context": correlationBlock(true),
		})

		edges := correlationEdgesOf(envelope)
		if len(edges) == 0 {
			t.Fatalf("no %s edge; edges: %s", correlation.EdgeCorrelatesWith, edgeSummary(envelope))
		}
		e := edges[0]
		if e["method"] != correlation.CorrelationMethod {
			t.Errorf("method %v, want %q", e["method"], correlation.CorrelationMethod)
		}
		// THE EVIDENCE NAMES BOTH SERVICES AND BOTH RESOURCES, which is what
		// ties the edge to the same resolutions the EMITTED_BY edges point at.
		evidence, _ := e["evidence"].(string)
		for _, want := range []string{correlationServiceA, correlationServiceB, "score="} {
			if !strings.Contains(evidence, want) {
				t.Errorf("the evidence %q does not name %q", evidence, want)
			}
		}
		if score, ok := e["confidence"].(float64); !ok || score <= 0 {
			t.Errorf("confidence %v, want the co-occurrence score", e["confidence"])
		}
		// The two templates it joins are different, and each is a real node of
		// this graph rather than an id from nowhere.
		if e["from_id"] == e["to_id"] {
			t.Error("the edge joins a template to itself")
		}
		for _, endpoint := range []string{"from_id", "to_id"} {
			id, _ := e[endpoint].(string)
			if !hasNode(envelope, id, nodeLogTemplate) {
				t.Errorf("the edge's %s %q is not a log-template node of this graph", endpoint, id)
			}
		}
	})

	// THE DECLARED EDGE RUNS THE OTHER WAY, and the pair must still confirm.
	// Connection is symmetric even where a declared edge is not, and the
	// direction the detector happens to ask in is not the operator's to
	// predict.
	t.Run("the declared edge declared in the REVERSE direction still confirms", func(t *testing.T) {
		child := dialStdioProvider(t, modeCorrelate)
		envelope := callForEnvelope(t, child, map[string]any{
			"id": "instance-1", "params": params, "context": correlationBlockDirected(true, true),
		})
		if edges := correlationEdgesOf(envelope); len(edges) == 0 {
			t.Errorf("a dependency declared from %s to %s confirmed nothing, though the same edge declared "+
				"the other way does; edges: %s", correlationServiceB, correlationServiceA, edgeSummary(envelope))
		}
	})

	// CONTROL ONE, and it is the arm that makes the edge a claim about the
	// operator's cloud graph rather than about two services logging at once.
	// The same events and the same declared NODES, with the edge between them
	// removed: the pair is still detected and stays unconfirmed, so nothing is
	// emitted.
	t.Run("the same events with NO declared edge emit nothing", func(t *testing.T) {
		child := dialStdioProvider(t, modeCorrelate)
		envelope := callForEnvelope(t, child, map[string]any{
			"id": "instance-1", "params": params, "context": correlationBlock(false),
		})
		if edges := correlationEdgesOf(envelope); len(edges) != 0 {
			t.Errorf("an unconfirmed pair produced %d %s edge(s): %s",
				len(edges), correlation.EdgeCorrelatesWith, edgeSummary(envelope))
		}
		// The proxies still appear, so the block WAS read and the zero above is
		// the missing dependency rather than a block that never arrived.
		if !hasType(envelope, nodeProxy) {
			t.Errorf("the declared nodes produced no proxy, so this control proves nothing: %s",
				nodeSummary(envelope))
		}
	})

	// CONTROL TWO: no block at all. Neither the proxies nor the correlation.
	t.Run("no declared block emits neither proxies nor correlations", func(t *testing.T) {
		child := dialStdioProvider(t, modeCorrelate)
		envelope := callForEnvelope(t, child, map[string]any{"id": "instance-1", "params": params})
		if edges := correlationEdgesOf(envelope); len(edges) != 0 {
			t.Errorf("a collect with no block produced %d correlation edge(s)", len(edges))
		}
		if hasType(envelope, nodeProxy) {
			t.Errorf("a collect with no block produced a proxy: %s", nodeSummary(envelope))
		}
		if !hasType(envelope, nodeLogTemplate) {
			t.Errorf("the walk produced no log nodes at all: %s", nodeSummary(envelope))
		}
	})
}

// correlationEdgesOf returns the envelope's correlation edges.
func correlationEdgesOf(envelope map[string]any) []map[string]any {
	var out []map[string]any
	for _, e := range rows(envelope, "edges") {
		if e["type"] == correlation.EdgeCorrelatesWith {
			out = append(out, e)
		}
	}
	return out
}

// TestThisModulesSeverityVocabularyIsTheCommonDetectorsToo is the seam pin
// between two copies of one vocabulary.
//
// THE FILTER MOVED AND THE SPELLING DID NOT. The common detector keeps only
// templates at ERROR or above, and it reads the severity string THIS module
// writes into a Template. Nothing in the compiler relates the two vocabularies,
// so a rename or a re-ranking on either side would silently stop correlating
// this collector's errors while every other test stayed green.
func TestThisModulesSeverityVocabularyIsTheCommonDetectorsToo(t *testing.T) {
	levels := []string{SeverityTrace, SeverityDebug, SeverityInfo, SeverityWarn, SeverityError, SeverityCritical}
	common := []string{
		correlation.SeverityTrace, correlation.SeverityDebug, correlation.SeverityInfo,
		correlation.SeverityWarn, correlation.SeverityError, correlation.SeverityCritical,
	}
	for i, name := range levels {
		if name != common[i] {
			t.Errorf("severity %d is %q here and %q in the common detector", i, name, common[i])
		}
	}
	for _, level := range levels {
		for _, min := range levels {
			if got, want := correlation.SeverityAtLeast(level, min), SeverityAtLeast(level, min); got != want {
				t.Errorf("SeverityAtLeast(%q, %q) = %v in the common detector and %v here", level, min, got, want)
			}
		}
	}
	// THE ONE COMPARISON THE FILTER MAKES, stated on its own so a reader sees
	// which way it must go.
	if !correlation.SeverityAtLeast(SeverityError, correlation.SeverityError) {
		t.Error("this module's ERROR does not satisfy the common detector's ERROR minimum")
	}
	if correlation.SeverityAtLeast(SeverityWarn, correlation.SeverityError) {
		t.Error("this module's WARN satisfies the common detector's ERROR minimum")
	}
}

// TestTheCorrelationEdgeTypeIsTheCommonModulesToo pins the second shared
// spelling. This module names the type in its own edge vocabulary for a reader's
// benefit, and the value comes from the module that emits it.
func TestTheCorrelationEdgeTypeIsTheCommonModulesToo(t *testing.T) {
	if edgeCorrelatesWith != correlation.EdgeCorrelatesWith {
		t.Errorf("this module names the edge %q and the common module emits %q",
			edgeCorrelatesWith, correlation.EdgeCorrelatesWith)
	}
}

// TestTheDetectorRefusesThisModulesMalformedOutput pins that the detector's
// bad-input contract is live at this module's own call site.
//
// WHAT IT DOES NOT REACH, stated rather than left to be noticed: buildGraph's own
// propagation of that error. No entry set can drive this pipeline into producing
// input the detector refuses — drain gives every template a non-empty id, a
// cluster's FirstSeen is its minimum and its LastSeen its maximum, and neither
// buildStreams nor assembleChunks emits a nil element — so the branch is
// defensive against a future change to THIS pipeline rather than against any
// input a collect can carry. It is kept because the alternative is a walk that
// ships a graph with its correlation half silently missing; it is named here
// because a branch no test can reach is a liability a reader should know about
// rather than discover.
func TestTheDetectorRefusesThisModulesMalformedOutput(t *testing.T) {
	for _, tc := range []struct {
		name      string
		templates []*LogTemplate
		chunks    []*LogChunk
		streams   []*LogStream
	}{
		{"a template with an empty id", []*LogTemplate{{ID: "", Severity: SeverityError}}, nil, nil},
		{"a nil template", []*LogTemplate{nil}, nil, nil},
		{
			"a nil chunk",
			[]*LogTemplate{{ID: "t1", Severity: SeverityError}},
			[]*LogChunk{nil},
			[]*LogStream{{ID: "s1", Labels: map[string]string{"service": "a"}}},
		},
		{
			"a stream with an empty id",
			[]*LogTemplate{{ID: "t1", Severity: SeverityError}},
			nil,
			[]*LogStream{{ID: "", Labels: map[string]string{"service": "a"}}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := findCorrelations(tc.templates, tc.chunks, tc.streams, nil, CloudContext{}); err == nil {
				t.Fatalf("%s was accepted", tc.name)
			}
		})
	}

	// THE CONTROL: well-formed input of the same shape is accepted, so the
	// refusals above are about the malformation and not about the call.
	if _, err := findCorrelations(
		[]*LogTemplate{{ID: "t1", Severity: SeverityError}},
		[]*LogChunk{{StreamID: "s1", TemplateID: "t1"}},
		[]*LogStream{{ID: "s1", Labels: map[string]string{"service": "a"}}},
		nil, CloudContext{},
	); err != nil {
		t.Fatalf("well-formed input was refused: %v", err)
	}
}
