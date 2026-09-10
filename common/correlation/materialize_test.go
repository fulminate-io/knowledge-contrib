// SPDX-License-Identifier: Apache-2.0

package correlation

import "testing"

// materialize_test.go — THE EMITTED EDGE: the two parity arms
// materialize_proxies_test.go carries at the parity tree, plus the empty-set
// arm that pins the nil return.

// TestMaterializeCorrelations_PreservesCorrelationConfidence is the parity
// target's materialize_proxies_test.go:91 arm: a confirmed correlation yields a
// CORRELATES_WITH edge whose Confidence IS the co-occurrence score, with the
// method and evidence the built-in emits.
func TestMaterializeCorrelations_PreservesCorrelationConfidence(t *testing.T) {
	edges := MaterializeCorrelations([]Result{{
		TemplateA: "tplA", TemplateB: "tplB",
		ServiceA: "svcA", ServiceB: "svcB",
		ResourceA: "resA", ResourceB: "resB",
		CooccurrenceScore: 0.42, StructurallyConfirmed: true,
	}})
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge, got %d: %+v", len(edges), edges)
	}
	e := edges[0]
	if e.Type != EdgeCorrelatesWith {
		t.Errorf("Type = %q, want %q", e.Type, EdgeCorrelatesWith)
	}
	if e.FromID != "tplA" || e.ToID != "tplB" {
		t.Errorf("endpoints = %q → %q", e.FromID, e.ToID)
	}
	if diff := e.Confidence - 0.42; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("Confidence = %v, want the co-occurrence score 0.42", e.Confidence)
	}
	if e.Method != CorrelationMethod {
		t.Errorf("Method = %q, want %q", e.Method, CorrelationMethod)
	}
	// THE EXPECTATION IS A LITERAL, not a value read back from the producer: this
	// string is the informal contract the golden and every consumer read.
	if want := "services=svcA,svcB resources=resA,resB score=0.420"; e.Evidence != want {
		t.Errorf("Evidence = %q, want %q", e.Evidence, want)
	}
}

// TestMaterializeCorrelations_SkipsUnconfirmedCorrelations is the parity
// target's materialize_proxies_test.go:125 arm: an unconfirmed candidate emits
// no edge at all.
func TestMaterializeCorrelations_SkipsUnconfirmedCorrelations(t *testing.T) {
	edges := MaterializeCorrelations([]Result{
		{TemplateA: "tplA", TemplateB: "tplB", StructurallyConfirmed: false},
		{TemplateA: "tplC", TemplateB: "tplD", CooccurrenceScore: 1, StructurallyConfirmed: true},
	})
	for _, e := range edges {
		if e.FromID == "tplA" {
			t.Errorf("an unconfirmed correlation emitted an edge: %+v", e)
		}
	}
	if len(edges) != 1 {
		t.Fatalf("expected only the confirmed edge, got %d: %+v", len(edges), edges)
	}
	// AN EMPTY CORRELATION SET EMITS NIL, not an empty slice: a caller appending
	// the result of a collect that correlated nothing appends nothing, and the
	// distinction is what the two existing materializers already carry.
	if edges := MaterializeCorrelations(nil); edges != nil {
		t.Errorf("an empty correlation set must emit nil, got %+v", edges)
	}
	if edges := MaterializeCorrelations([]Result{}); edges != nil {
		t.Errorf("an empty correlation set must emit nil, got %+v", edges)
	}
}
