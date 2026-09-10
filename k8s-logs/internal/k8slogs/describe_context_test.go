// SPDX-License-Identifier: Apache-2.0

package k8slogs

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// describe_context_test.go — THE RENDERED FOREIGN-GRAPH DECLARATION, read off
// the value this collector actually serves.
//
// WHY THIS FILE EXISTS. The rest of this module's describe rows assert the
// declaration against the symbols it is built from, which cannot see a render
// that is self-consistently wrong. The foreign-context half has exactly that
// failure mode: it is a correlation input, so a declaration that selects nothing
// produces an empty slice, no error, and a collect that reports success having
// correlated against nothing. Both shapes this collector has shipped were of
// that kind — a node type of "<family>-resource" that no provider emits, and a
// field set that empties when the per-family declaration it was derived from
// loses its arms.
//
// SO THE ROWS BELOW READ Describe().Context AND NOTHING ELSE, and they name the
// values rather than recomputing them: the five metadata keys are the ones the
// correlation matches on, and a render that lost one would still be
// self-consistent with whatever produced it.

// correlationMetadataKeys is what a stream's resource must carry for this
// collector to correlate against it. It is written here rather than read from
// the renderer, because a row that read the renderer's own list could not tell a
// dropped key from a key nobody declared.
var correlationMetadataKeys = []string{"namespace", "cluster_name", "resource_type", "region", "provider"}

// TestDescribedForeignContext_CarriesTheWholeCorrelationSlicePerFamily is row
// 29a's assertion (a): every declared family selects EVERY node type, carries
// the node fields the edge read pivots on, the five metadata keys the
// correlation matches on, and both edge fields.
func TestDescribedForeignContext_CarriesTheWholeCorrelationSlicePerFamily(t *testing.T) {
	declared := (&Collector{}).Describe().Context
	if len(declared) == 0 {
		t.Fatal("this collector declares no foreign-graph context at all; every row below would pass vacuously")
	}
	for _, family := range []string{"aws", "azure", "gcp", "k8s"} {
		decl, present := declared[family]
		if !present {
			t.Errorf("the rendered declaration names no %q family; this collector correlates against it", family)
			continue
		}
		if !decl.AllNodeTypes {
			t.Errorf("family %q does not set all_node_types; naming types instead is what selected nothing on every "+
				"family this collector correlates against — three emit cloud-resource and one emits a type per resource kind",
				family)
		}
		if len(decl.NodeTypes) != 0 {
			t.Errorf("family %q sets all_node_types AND names node types %v; the client refuses that declaration by name",
				family, decl.NodeTypes)
		}
		if !slices.Contains(decl.NodeFields, "id") {
			t.Errorf("family %q declares node fields %v, which omit `id`; the edge read is pivoted on the carried "+
				"nodes' ids, so without it the family receives zero edges and no error", family, decl.NodeFields)
		}
		if !slices.Contains(decl.NodeFields, "type") {
			t.Errorf("family %q declares node fields %v, which omit `type`; the correlation falls back to the node's "+
				"type when a resource carries no resource_type metadata", family, decl.NodeFields)
		}
		for _, key := range correlationMetadataKeys {
			if !slices.Contains(decl.MetadataKeys, key) {
				t.Errorf("family %q does not declare the metadata key %q, which the correlation matches on; a stream "+
					"whose resource carries it would not be matched and nothing would say so", family, key)
			}
		}
		for _, field := range []string{"from_id", "to_id"} {
			if !slices.Contains(decl.EdgeFields, field) {
				t.Errorf("family %q does not declare the edge field %q; the edges are what CONFIRM a temporal "+
					"correlation, so without them this collector emits correlations it cannot stand behind",
					family, field)
			}
		}
	}
}

// TestDescribedForeignContext_RendersTheEntrysOwnShape is row 29a's assertion
// (b) as far as this module can carry it: the rendered value marshals to the
// MAP shape the client's loader decodes, with no `reason` key at any level.
//
// THE VALIDATE HALF IS THE CLIENT'S. A collector module cannot import the
// client's internal package, so the assertion that this document passes
// ContextDeclaration.Validate lives beside that validator, against the checked-in
// copy of this render (cmd/knowledge/internal/collectorconfig/testdata/
// k8s-logs-context.json). The row below is what keeps that copy honest: it
// fails if the render's shape changes without the copy changing with it.
func TestDescribedForeignContext_RendersTheEntrysOwnShape(t *testing.T) {
	raw, err := json.Marshal((&Collector{}).Describe().Context)
	if err != nil {
		t.Fatalf("marshaling the rendered declaration: %v", err)
	}
	var doc map[string]map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("the rendered declaration is not an object keyed by family: %v", err)
	}
	for family, arm := range doc {
		if _, present := arm["reason"]; present {
			t.Errorf("family %q renders a `reason` key; the client's loader decodes with DisallowUnknownFields and "+
				"refuses the whole scoped file by name", family)
		}
		if arm["all_node_types"] != true {
			t.Errorf("family %q renders all_node_types=%v", family, arm["all_node_types"])
		}
	}
	// EVERY FAMILY THIS COLLECTOR DECLARES IS IN THE DOCUMENT, which is what
	// makes the per-family rows above a census rather than a spot check.
	if len(doc) != len(cloudProviderFamilies()) {
		t.Errorf("the rendered document carries %d families and this collector declares %d",
			len(doc), len(cloudProviderFamilies()))
	}
}

// TestDescribedForeignContext_IsNotDerivedFromTheStrippableDeclaration is the
// REBASE row, and it is the one that would have caught the defect this file was
// written for.
//
// THE COUPLING IT REFUSES. describedForeignContext once took its node fields,
// metadata keys and edge fields from DeclaredForeignContext()'s per-family
// value. That value is a different declaration with a different job — it is what
// this module publishes for review — and a sibling change that empties one of
// its arms silently empties the render. Measured against ticket 39's landed
// values, the gcp arm becomes the selector with three empty halves.
//
// SO THE RENDER STATES THE SLICE ITSELF, and this row proves it: the render is
// unchanged when the per-family declaration carries nothing at all.
func TestDescribedForeignContext_IsNotDerivedFromTheStrippableDeclaration(t *testing.T) {
	before := (&Collector{}).Describe().Context

	stripped := framework.ForeignContextDeclaration{}
	for _, family := range cloudProviderFamilies() {
		stripped[family] = framework.ForeignFamilyDeclaration{Reason: "stripped by a sibling change"}
	}
	after := foreignContextFrom(stripped)

	for _, family := range cloudProviderFamilies() {
		got, want := after[family], before[family]
		if !equalFamilyDeclaration(got, want) {
			t.Errorf("family %q renders differently when the published declaration is emptied:\n after: %+v\nbefore: %+v",
				family, got, want)
		}
	}
}

// equalFamilyDeclaration compares two rendered family declarations by value.
func equalFamilyDeclaration(a, b framework.ForeignFamilyDeclaration) bool {
	return a.AllNodeTypes == b.AllNodeTypes &&
		slices.Equal(a.NodeTypes, b.NodeTypes) &&
		slices.Equal(a.NodeFields, b.NodeFields) &&
		slices.Equal(a.MetadataKeys, b.MetadataKeys) &&
		slices.Equal(a.EdgeFields, b.EdgeFields) &&
		slices.Equal(a.PathBasenames, b.PathBasenames)
}
