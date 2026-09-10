// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// declaration_test.go — the DECLARE side of this module's foreign-graph context,
// and the README that ships it.
//
// THIS MODULE HAD NO DECLARATION AT ALL. It consumed a foreign context on every
// collect and asked for none, so the block always arrived empty and every proxy,
// EMITTED_BY edge and CORRELATES_WITH edge the cloud half can emit was
// unreachable in production while the read-side suite stayed green against
// blocks the tests handed it. The rows below hold the two halves that were
// missing: the declaration exists and names the fields the read side actually
// reads, and the README ships exactly that declaration.
//
// NO ASSERTION LIBRARY, and that is a constraint rather than a style: this
// collector is PUBLISHED STANDALONE and the workspace's standalone census builds
// and tests it with the workspace off, so a test-only dependency here is a
// dependency its operators acquire.

func TestDeclaredForeignContext_NamesTheFieldsTheResolverMatchesOn(t *testing.T) {
	decl := DeclaredForeignContext()
	if len(decl) == 0 {
		t.Fatal("this module reads a foreign context on every collect, so it declares one")
	}

	for _, family := range decl.Families() {
		d := decl[family]
		if d.Reason == "" {
			t.Errorf("the %q entry owes a reason a reviewer can check", family)
		}

		// THE EXTERNAL EXPECTATION this module can state without importing another
		// module: a declared node type is never the family name with a suffix. That
		// is the shape four sibling declarations shipped with, and it is a string no
		// collector emits.
		if slices.Contains(d.NodeTypes, family+"-resource") {
			t.Errorf("the %q entry declares the family name with a suffix rather than a type that "+
				"collector emits; that spelling selects zero nodes", family)
		}
		if !slices.Equal(d.NodeTypes, []string{"cloud-resource"}) {
			t.Errorf("the %q entry declares %v; it must name the one node type that inventory "+
				"collector emits", family, d.NodeTypes)
		}

		// THE THREE VALUES matchableNames INDEXES ON. `symbol_name` is a node field;
		// `name` and `service` are metadata keys, and an undeclared key arrives
		// ABSENT rather than empty, so a declaration missing them indexes every
		// candidate under its symbol name alone.
		requireContains(t, family, "node_fields", d.NodeFields, "symbol_name")
		requireContains(t, family, "metadata_keys", d.MetadataKeys, metaName)
		requireContains(t, family, "metadata_keys", d.MetadataKeys, metaService)

		// THE ID, without which the edge read has no pivot and the client refuses
		// the declaration by name.
		requireContains(t, family, "node_fields", d.NodeFields, "id")
		requireContains(t, family, "edge_fields", d.EdgeFields, "from_id")
		requireContains(t, family, "edge_fields", d.EdgeFields, "to_id")

		if family == "cloud" || family == "logs" {
			t.Errorf("the %s family is retired; a declaration naming it is refused at collect time", family)
		}
	}

	// GCP IS ABSENT AND THAT IS ASSERTED rather than left implicit, because an
	// absent family otherwise reads as an oversight.
	if _, declared := decl["gcp"]; declared {
		t.Error("gcp emits its resource type as the node type; the only admissible spelling today " +
			"selects no nodes, so the family waits for an explicit family-level selector")
	}
}

func requireContains(t *testing.T, family, what string, list []string, want string) {
	t.Helper()
	if !slices.Contains(list, want) {
		t.Errorf("the %q entry's %s is %v and must declare %q", family, what, list, want)
	}
}

// TestTheDeclaredFieldSetIsTheOneTheResolverReads is the seam row, and the
// mutation that proves it.
//
// A block is built FROM the declaration — a field the declaration does not name
// is ABSENT from the node, exactly as the client's projection leaves it — and
// handed to this module's real resolver. Then the same thing with the k8s-logs
// collector's metadata keys substituted, which is the copy-paste a reader of that
// module's declaration would most plausibly make.
func TestTheDeclaredFieldSetIsTheOneTheResolverReads(t *testing.T) {
	const resourceName = "checkout"

	build := func(decl framework.ForeignContextDeclaration) cloudContext {
		var graphs []framework.ForeignGraph
		for _, family := range decl.Families() {
			d := decl[family]
			node := framework.ForeignNode{}
			for _, field := range d.NodeFields {
				if field == "id" {
					node.ID = family + ":res-1"
				}
				// symbol_name is deliberately NOT filled: this row is about the
				// metadata half, and a node carrying its name in both places would
				// resolve under either declaration and prove nothing.
			}
			for _, key := range d.MetadataKeys {
				if node.Metadata == nil {
					node.Metadata = map[string]string{}
				}
				node.Metadata[key] = "declared-but-unused"
			}
			if _, declared := node.Metadata[metaName]; declared {
				node.Metadata[metaName] = resourceName
			}
			graphs = append(graphs, framework.ForeignGraph{
				GraphName: family + "-acct", Nodes: []framework.ForeignNode{node},
			})
		}
		return newForeignCloudContext(graphs)
	}

	own := build(DeclaredForeignContext())
	if own == nil {
		t.Fatal("the block filled from this module's own declaration is empty")
	}
	if _, resolved := own.ResolveService(nil, "service", resourceName); !resolved {
		t.Error("a resource whose NAME metadata equals the label value must resolve under this " +
			"module's own declaration — if it does not, the declaration and the resolver disagree")
	}

	// THE MUTATION: k8s-logs' metadata keys, which omit `name` and `service`.
	mutated := framework.ForeignContextDeclaration{}
	for family, d := range DeclaredForeignContext() {
		d.MetadataKeys = []string{"namespace", "cluster_name", "resource_type", "region", "provider"}
		mutated[family] = d
	}
	borrowed := build(mutated)
	if borrowed == nil {
		t.Fatal("control: the nodes must still arrive; only the keys the resolver reads are missing")
	}
	if _, resolved := borrowed.ResolveService(nil, "service", resourceName); resolved {
		t.Error("a declaration carrying the k8s-logs field set resolved a name it never asked for; " +
			"those keys drop `name` and `service`, which are the two this module matches on")
	}
}

// TestTheREADMEShipsTheDeclaredContextBlock is the pin. This module renders no
// config entry in Go, so its README block is the only artifact carrying the entry
// an operator installs — and a hand-written block with no generator to drift from
// is a block nothing can tell is wrong.
func TestTheREADMEShipsTheDeclaredContextBlock(t *testing.T) {
	fragment, err := RenderedContextBlock()
	if err != nil {
		t.Fatalf("rendering the declared context block: %v", err)
	}

	body, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("this module's README could not be read: %v", err)
	}
	if len(body) < 2000 {
		t.Fatalf("README.md is %d bytes, too short to be this module's documentation", len(body))
	}
	if !strings.Contains(string(body), fragment) {
		t.Fatalf("the README's worked entry does not carry this module's declared context block. "+
			"Paste this into the entry:\n\n%s", fragment)
	}

	// AND THE PROSE, because the `reason` on each declaration is a Go field the
	// entry never renders: the client's decoder has no field for it and refuses an
	// unknown key by name, failing the whole config file.
	for _, phrase := range []string{"EMITTED_BY", "gcp", "symbol_name"} {
		if !strings.Contains(string(body), phrase) {
			t.Errorf("the README ships a context block whose entries carry no reason on the wire, so "+
				"it owes the reader %q in prose", phrase)
		}
	}
}

// TestTheRenderedBlockCarriesNoGraphOrReasonKey is the loader-shape pin. The
// client decodes `context` into a map keyed by family and calls
// DisallowUnknownFields; a `graph` or `reason` key is refused BY NAME and the
// refusal fails the operator's whole config file, taking every other collector
// they registered down with it.
func TestTheRenderedBlockCarriesNoGraphOrReasonKey(t *testing.T) {
	body, err := json.Marshal(DeclaredForeignContext())
	if err != nil {
		t.Fatalf("marshaling the declaration: %v", err)
	}

	var decoded map[string]map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decoding the rendered declaration: %v", err)
	}
	if len(decoded) == 0 {
		t.Fatal("the rendered declaration is empty")
	}
	for family, keys := range decoded {
		for _, banned := range []string{"graph", "reason"} {
			if _, present := keys[banned]; present {
				t.Errorf("the rendered %q entry carries the %q key, which the client's decoder "+
					"refuses by name", family, banned)
			}
		}
	}

	// THE REASON SURVIVES ON THE GO VALUE, where a reviewer and the census read
	// it. Asserting both halves keeps "not rendered" from drifting into "not
	// recorded".
	for family, d := range DeclaredForeignContext() {
		if d.Reason == "" {
			t.Errorf("the %q declaration lost its reason on the Go value", family)
		}
		if strings.Contains(string(body), d.Reason) {
			t.Errorf("the %q reason reached the rendered entry", family)
		}
	}
}
