// SPDX-License-Identifier: Apache-2.0

package k8slogs

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// declaration_wiring_test.go — THE DECLARE SIDE OF R12, which is the half a
// read-side test cannot reach.
//
// WHAT WAS WRONG, FIRST TIME. DeclaredForeignContext existed, compiled, and was
// called by nothing. A declaration with no consumer cannot produce the
// observable R12 names, so the example entry now carries it.
//
// WHAT WAS WRONG, SECOND TIME, AND IT IS THE REASON THIS FILE WAS REWRITTEN. The
// entry carried the declaration and the declaration named node types NO
// COLLECTOR EMITS — `aws-resource`, `azure-resource`, `gcp-resource`,
// `k8s-resource`, computed as `family + "-resource"` — while the aws, azure and
// k8s collectors emit `cloud-resource` and gcp emits its resource type as the
// node type. The client's fill filters by exact string equality, so every family
// arrived with zero nodes and the whole correlation half resolved against
// nothing, silently. The assertion standing here asserted the SAME EXPRESSION
// the declaration computed, so it pinned the defective spelling rather than
// catching it: a hand-written copy of the production value proves the value has
// not changed, never that it is right.
//
// SO THE VOCABULARY CHECK IS NOT HERE, AND CANNOT BE. The emitters live in
// separate Go modules this one cannot import. cmd/contextcensus, in the root
// module, reads their node-type constants from source and holds every declared
// string against them, reddening BY NAME on a type no module emits. This file
// pins what it legitimately can: the shape an operator's loader will accept, the
// fields the read side matches on, and the one property that is external to the
// declaration — that no declared type is derived from the family name, which is
// exactly the shape that shipped broken.

func TestDeclaredForeignContext_NamesTheProviderFamiliesAndTheFieldsTheReadNeeds(t *testing.T) {
	decl := DeclaredForeignContext()
	require.Len(t, decl, len(cloudProviderFamilies()),
		"one declaration per provider family this collector can correlate against")

	for _, family := range cloudProviderFamilies() {
		d, ok := decl[family]
		require.Truef(t, ok, "no declaration for the %q family", family)

		require.NotEmptyf(t, d.Reason, "the %q entry owes a reason a reviewer can check", family)

		// THE EXTERNAL EXPECTATION, and the only one this module can state without
		// importing another module: a declared node type is NEVER the family name
		// with a suffix. That is the exact shape this collector shipped for four
		// families, and it is a shape no collector in the tree emits — a type
		// string computed from the asker's own vocabulary rather than read from the
		// producer's.
		for _, nodeType := range d.NodeTypes {
			assert.NotEqualf(t, family+"-resource", nodeType,
				"the %q entry declares %q, which is the family name with a suffix rather than a type "+
					"any collector emits; that spelling selected zero nodes in every family and lost "+
					"every cross-graph edge", family, nodeType)
		}

		if family == familyGCP {
			// THE GCP ENTRY IS THE FAMILY KEY AND NOTHING ELSE, and every other
			// field is asserted ABSENT rather than left unstated: the client refuses
			// node fields, metadata keys or edge fields declared without node types,
			// and that refusal fails the operator's whole config file.
			assert.Emptyf(t, d.NodeTypes, "the %q entry declares no node types until a family-level selector exists", family)
			assert.Emptyf(t, d.NodeFields, "the %q entry declares no node fields; the client refuses them without node types", family)
			assert.Emptyf(t, d.MetadataKeys, "the %q entry declares no metadata keys; the client refuses them without node types", family)
			assert.Emptyf(t, d.EdgeFields, "the %q entry declares no edge fields; the client refuses them without node types", family)
			assert.Containsf(t, d.Reason, "NAMES ONLY",
				"the %q entry's reason must DISCLOSE that it selects nothing, not justify a selection it does not make", family)
			continue
		}

		assert.Equalf(t, []string{nodeTypeCloudResource}, d.NodeTypes,
			"the %q entry names the node type that collector actually emits", family)

		// THE ID FIELD IS LOAD-BEARING, not decorative: EMITTED_BY runs FROM the
		// resource node's id, and contextEdgePivotIDs on the client skips a node
		// whose ID is empty — so a declaration without it yields a block that
		// carries nodes and produces no edges, silently.
		assert.Containsf(t, d.NodeFields, "id",
			"the %q entry must declare the id node field, or the correlation has no endpoint", family)

		for _, key := range []string{cloudMetaNamespace, cloudMetaCluster, cloudMetaResourceType} {
			assert.Containsf(t, d.MetadataKeys, key,
				"the %q entry must declare %q — it is what a log stream's labels are matched against",
				family, key)
		}

		// THE EDGE ENDPOINTS. Dependencies reads both to CONFIRM a correlation,
		// so a declaration carrying edge_fields without them confirms nothing.
		assert.Containsf(t, d.EdgeFields, "from_id", "the %q entry declares the edge source", family)
		assert.Containsf(t, d.EdgeFields, "to_id", "the %q entry declares the edge target", family)
	}

	// NO DECLARATION NAMES A RETIRED FAMILY. The client refuses one by name at
	// collect time, so an entry generated from this function would fail at the
	// operator's first collect rather than here — which is exactly why it is
	// asserted here.
	for family := range decl {
		assert.NotContains(t, []string{"cloud", "logs"}, family,
			"the %s family is retired; a declaration naming it is refused at collect time", family)
	}
}

// TestExampleEntry_CarriesTheDeclaredContext is the WIRING assertion, and it is
// the one that was missing. Without the context key in the entry, the client
// supplies nothing and every read-side test above it is testing a block that
// never arrives.
func TestExampleEntry_CarriesTheDeclaredContext(t *testing.T) {
	raw, err := ExampleEntry(OSLinux, false, "/home/you/.knowledge/bin/knowledge-collector-k8s-logs")
	require.NoError(t, err)

	var doc struct {
		Collectors map[string]struct {
			Tool    string                     `json:"tool"`
			Context map[string]json.RawMessage `json:"context"`
		} `json:"collectors"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &doc))

	entry, ok := doc.Collectors[GraphFamily]
	require.Truef(t, ok, "the example entry is keyed by the graph family %q", GraphFamily)

	// THE ENTRY NAMES THIS MODULE'S OWN TOOL, and that is asserted rather than
	// assumed because the eight collector modules DO NOT SERVE A UNIFORM TOOL
	// NAME: five serve `collect`, while aws serves collect_aws, loki serves
	// collect_loki_logs and this one serves collect_k8s_logs. An entry naming a
	// tool the provider does not serve is refused at registration with a mismatch
	// the operator has to debug, so a copied example is the likeliest way to
	// produce one.
	assert.Equal(t, ToolName, entry.Tool,
		"the generated entry must name the tool THIS collector serves, not the common spelling")
	assert.Equal(t, ToolName, (&Collector{}).Tool().Name,
		"and that constant must still be what the collector advertises over the protocol")
	require.NotEmpty(t, entry.Context,
		"the example config entry carries NO context declaration, so an operator who installs it "+
			"receives no foreign-graph block and every correlation this collector confirms with "+
			"cloud resources resolves against nothing")

	// THE SHAPE THE OPERATOR'S LOADER ACCEPTS, and this is the assertion whose
	// absence let an unloadable entry ship. The client decodes `context` into a
	// MAP KEYED BY FAMILY and calls DisallowUnknownFields; the entry rendered an
	// ARRAY of objects carrying `graph` and `reason`, so the loader refused it —
	// and a refusal fails the WHOLE config file, taking every other collector the
	// operator registered with it. Decoding into a map here is that pin: an array
	// no longer unmarshals into it.
	for _, family := range cloudProviderFamilies() {
		require.Containsf(t, entry.Context, family, "the rendered block is keyed by family and names %q", family)
	}
	for family, body := range entry.Context {
		var keys map[string]any
		require.NoErrorf(t, json.Unmarshal(body, &keys), "the %q entry is an object", family)
		for _, banned := range []string{"graph", "reason"} {
			assert.NotContainsf(t, keys, banned,
				"the rendered %q entry carries the %q key, which the client's entry decoder has no field "+
					"for and refuses BY NAME — taking every other collector in the operator's config file "+
					"down with it", family, banned)
		}
	}

	// AND THE REASON SURVIVES WHERE A REVIEWER READS IT: on the Go value, never on
	// the wire. Asserting both halves keeps "not rendered" from drifting into "not
	// recorded".
	for family, d := range DeclaredForeignContext() {
		assert.NotEmptyf(t, d.Reason, "the %q declaration keeps its reason on the Go value", family)
		assert.NotContainsf(t, raw, d.Reason,
			"the %q reason must not reach the rendered entry", family)
	}

	// THE GCP ENTRY RENDERS AS THE BARE FAMILY KEY. `{}` is the only spelling the
	// client's validator admits for "the graph names and nothing else": node
	// fields, metadata keys or edge fields without node types are each refused by
	// name.
	var gcp map[string]any
	require.NoError(t, json.Unmarshal(entry.Context[familyGCP], &gcp))
	assert.Emptyf(t, gcp, "the %q entry renders as the bare family key; it declares no node types", familyGCP)
}

// TestTheREADMEDisclosesTheGCPEntrySelectsNothing is the prose half of the same
// disclosure. The Reason is not rendered into the entry, so the README is where
// an operator reading the block learns that the gcp arm resolves nothing — and
// without this pin the block ships beside prose that says the opposite.
func TestTheREADMEDisclosesTheGCPEntrySelectsNothing(t *testing.T) {
	body, err := os.ReadFile(readmePath)
	require.NoError(t, err, "this module's README could not be read")
	for _, phrase := range []string{"no node types", "gcp"} {
		assert.Containsf(t, strings.ToLower(string(body)), phrase,
			"the README must say the gcp entry declares %s and therefore resolves nothing", phrase)
	}
}

// THE README PIN IS NOT DUPLICATED HERE. env_test.go's
// TestTheREADMEShipsTheGeneratedEntry already asserts the README ships exactly
// what ExampleEntry renders, so once the context key is in the generated entry
// that test reds until the README is regenerated — which is the third artifact
// this file's header names, held by the pin that already exists rather than by a
// second copy of it.
