// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"encoding/json"
	"strings"
	"testing"
)

// edge_marshal_test.go — THE BYTES AN EDGE MARSHALS TO, which is the one
// property of the two graph-family fields no other gate in this module can see.
//
// WHY IT EXISTS, MEASURED RATHER THAN ASSUMED. This module's two contract gates
// read the field NAME and nothing else: the schema-to-struct gate splits the
// json tag at the comma and keeps the part before it, and the byte-compare
// compares two schema files to each other. So a tag that lost its `omitempty`
// passes both — and every collector in the tree keeps passing too, because the
// module suites compare Go structures rather than the edge's JSON. What would
// actually happen is that every edge every collector emits gains a
// `"source_graph":""` (or `"target_graph":""`) key it never had, which is the
// exact shape the client's own Edge doc says must never happen: absent and
// present-and-empty must be indistinguishable downstream.
//
// It is the sibling of the client's own outbound assertion in
// externalcollector's crossgraph_edge_test.go, on this side of the contract.

// TestEdgeMarshal_AGraphFamilyFieldIsAbsentUnlessSet drives both directions for
// both fields in one instrument: unset marshals to bytes with no key at all, and
// set marshals to bytes carrying the key with the family.
func TestEdgeMarshal_AGraphFamilyFieldIsAbsentUnlessSet(t *testing.T) {
	plain, err := json.Marshal(Edge{FromID: "A", ToID: "B", Type: "blocks"})
	if err != nil {
		t.Fatalf("marshaling an ordinary in-graph edge: %v", err)
	}
	for _, key := range []string{"source_graph", "target_graph"} {
		if strings.Contains(string(plain), key) {
			t.Errorf("an edge naming no graph family carries %q in its bytes: %s\n"+
				"every collector's every edge would change shape the day the field shipped, and a "+
				"present-but-blank family is a value the client must then tell apart from an absent one",
				key, plain)
		}
	}

	source, err := json.Marshal(Edge{FromID: "charts/api/Chart.yaml", ToID: "prod/Deployment/api", Type: "DEPLOYS", SourceGraph: "code"})
	if err != nil {
		t.Fatalf("marshaling a foreign-FROM edge: %v", err)
	}
	if !strings.Contains(string(source), `"source_graph":"code"`) {
		t.Errorf("an edge that SET SourceGraph does not carry it in its bytes: %s", source)
	}

	target, err := json.Marshal(Edge{FromID: "prod/Deployment/api", ToID: "api", Type: "BUILDS", TargetGraph: "code"})
	if err != nil {
		t.Fatalf("marshaling a foreign-TO edge: %v", err)
	}
	if !strings.Contains(string(target), `"target_graph":"code"`) {
		t.Errorf("an edge that SET TargetGraph does not carry it in its bytes: %s", target)
	}
}
