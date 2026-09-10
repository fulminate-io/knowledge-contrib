// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/fulminate-io/knowledge-contrib/framework"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// registration_test.go — R2-c: the SHIPPED RECORD'S CONTENTS.
//
// THIS ROW IS ABOUT THE ENTRY, NOT ABOUT WHAT THE SERVER PERSISTS. The two are
// different objects: the runtime record the loader builds carries the provider
// and its environment as name-to-value pairs and never crosses the wire, and
// the persisted record carries the name and the behavior and no connection
// details at all — because the env block holds VALUES, and persisting it would
// put operator secrets into a graph-resident node that syncs. Everything
// asserted here is entry-side.
//
// R2-c2 — that the indexing opt-in actually REACHES the index — is a different
// observation needing a running client, a server and a drained search arm, and
// it is the live confirmation's row rather than this one. The two are not
// folded: a correct entry whose opt-in never reached the index would pass this
// file and read zero forever.

func TestRegistration_BehaviorDeclaresAllThreeFlagsAndItsFieldLists(t *testing.T) {
	b := ExampleEntry("linux", "/usr/local/bin/knowledge-collector-k8s").Behavior

	// A graph registered without the opt-in is collected, stored and walkable
	// and never enters the text index — the search reads zero and keeps reading
	// zero, which looks like nothing at all rather than like a failure.
	assert.True(t, b.Summarizable, "summarizable")
	assert.True(t, b.Embeddable, "embeddable")
	assert.True(t, b.Syncable, "syncable")

	// The three field lists are what to index. An empty one is not a sensible
	// default: it means the flag is on and nothing is indexed.
	assert.NotEmpty(t, b.BM25Fields, "bm25_fields")
	assert.NotEmpty(t, b.SummarizeFields, "summarize_fields")
	assert.NotEmpty(t, b.EmbedFields, "embed_fields")

	assert.Contains(t, b.BM25Fields, "symbol_name",
		"a workload is searched for by name, so the name must be in the keyword index")
}

// TestRegistration_EnvBlockIsExactlyTheAllowlist is the assertion that keeps
// the documented example and the collector's own expectations from drifting.
// The block IS the child's whole environment, so a name in one and not the
// other is a variable this process never receives.
func TestRegistration_EnvBlockIsExactlyTheAllowlist(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows"} {
		t.Run(goos, func(t *testing.T) {
			entry := ExampleEntry(goos, "/usr/local/bin/collector")
			assert.Equal(t, envAllowlist(goos), declaredEnvNames(entry),
				"the entry's env block is exactly the declared allowlist for %s", goos)
		})
	}
}

// TestRegistration_EveryEnvValueIsAVariableReferenceNotALiteral. An operator
// must never be shown an example that invites them to paste a credential into a
// config file.
func TestRegistration_EveryEnvValueIsAVariableReferenceNotALiteral(t *testing.T) {
	entry := ExampleEntry("linux", "/usr/local/bin/collector")
	for name, value := range entry.Env {
		assert.Equal(t, "${"+name+":-}", value,
			"the value for %s is a defaulted variable reference, so the operator's own "+
				"environment supplies it and an unset one arrives absent", name)
	}
}

// TestRegistration_DeclaresExactlyTheForeignContextTheLinkageShapesNeed.
//
// A DECLARATION IS A REQUEST FOR DATA THE CLIENT WILL COPY IN, so a broad one
// costs the operator's privacy and the collect's size for nothing. Two of the
// four linkage shapes need NO foreign input at all, and nothing is declared for
// them.
func TestRegistration_DeclaresExactlyTheForeignContextTheLinkageShapesNeed(t *testing.T) {
	decls := declaredForeignContext()
	require.Len(t, decls, 2, "two declarations: DEPLOYS and the Azure identity shape")

	for family, d := range decls {
		assert.NotEmpty(t, d.Reason, "every declaration says why it is needed")
		assert.Contains(t, []string{framework.FamilyCode, familyAzure}, family,
			"a declaration names `code` or a REGISTERED graph type; the built-in cloud "+
				"family it used to name no longer exists")
	}

	// The DEPLOYS declaration must ask for the file node's ID and BODY, not
	// only its name: the edge runs FROM the chart file node and the chart name
	// is parsed out of the body, so a declaration carrying names alone could
	// not produce the edge's source endpoint at all.
	deploys, ok := decls[framework.FamilyCode]
	require.True(t, ok, "the DEPLOYS declaration is the code-family entry")
	require.NotEmpty(t, deploys.PathBasenames, "and it is the one narrowed by basename")
	assert.Contains(t, deploys.PathBasenames, "Chart.yaml")
	assert.Contains(t, deploys.NodeFields, "id")
	assert.Contains(t, deploys.NodeFields, "content")

	// The Azure identity declaration is the ONLY provider one, because IRSA and
	// GCP compose their targets from the ServiceAccount's own metadata.
	//
	// AND IT NAMES azure RATHER THAN A CLOUD-WIDE FAMILY, which is the substance
	// of the vocabulary change rather than a rename: the shape resolves against a
	// MANAGED IDENTITY, which the azure collector produces, so a broader
	// declaration would pull in aws and gcp resources no shape reads.
	provider := 0
	for family, d := range decls {
		if family == framework.FamilyCode {
			continue
		}
		provider++
		assert.Equal(t, familyAzure, family,
			"the one provider declaration names the azure graph type")
		assert.Equal(t, []string{azureNodeTypeCloudResource}, d.NodeTypes,
			"and the azure collector's own node type, not a retired cloud-wide one")

		// THE EXTERNAL EXPECTATION, and the only one this module can state
		// without importing another module: the declared type is NOT the family
		// name with a suffix. `azure-resource` is what shipped here, it is a
		// string no collector emits, and it is the shape of a type guessed from
		// the asker's own vocabulary rather than read from the producer's. The
		// cross-module census is what holds the string against the azure
		// module's own constant; this reds on the shape.
		assert.NotContains(t, d.NodeTypes, family+"-resource",
			"the %q entry declares the family name with a suffix rather than a type that "+
				"collector emits; that spelling selected zero nodes and lost every "+
				"WORKLOAD_IDENTITY edge", family)
	}
	assert.Equal(t, 1, provider,
		"only the Azure workload-identity shape reads a foreign provider graph; declaring "+
			"provider context for IRSA or GCP would ask for data no shape uses")

	// NO DECLARATION NAMES A RETIRED FAMILY. The client refuses one by name, so
	// an entry generated from this function would fail at the operator's first
	// collect rather than here — which is exactly why it is asserted here.
	for family := range decls {
		assert.NotContains(t, []string{"cloud", "logs"}, family,
			"the %s family is retired; a declaration naming it is refused at collect time", family)
	}
}

// TestRegistration_NoDeclarationRemainsForTheRetiredBuildsShape is the
// declaration side's locking direction.
//
// A DECLARATION IS THE COST OF A SHAPE, so a shape that no longer exists must
// not keep charging for its input. The BUILDS shape's whole input was the NAMES
// of the code graphs that exist, which it asked for with an entry carrying a
// family and nothing else; that entry is what a re-added predicate would need
// and what an operator would otherwise still pay for.
//
// THE SURVIVING code ENTRY IS NARROWED. It asks for Chart.yaml file nodes for
// DEPLOYS, so "no names-only code entry" is a real distinction here rather than
// a ban on the code family.
func TestRegistration_NoDeclarationRemainsForTheRetiredBuildsShape(t *testing.T) {
	decls := declaredForeignContext()
	require.Len(t, decls, 2, "two declarations: DEPLOYS and the Azure identity shape")

	assert.Zero(t, countNamesOnlyCodeDeclarations(decls),
		"the names-only code entry was the BUILDS shape's whole input and goes with it")

	// KNOWN POSITIVE ON THE PREDICATE, same run: it counts the retired entry
	// when one is present, so the zero above is an observation rather than a
	// predicate that can only return zero.
	retired := framework.ForeignContextDeclaration{
		framework.FamilyCode: {Reason: "the retired shape"},
	}
	assert.Equal(t, 1, countNamesOnlyCodeDeclarations(retired),
		"known positive: the predicate sees a names-only code entry when one is there")

	for _, d := range decls {
		assert.NotContains(t, d.Reason, retiredEdgeBuilds,
			"no surviving declaration is justified by an edge this collector does not emit")
	}
}

// countNamesOnlyCodeDeclarations counts entries naming the code family and
// asking for nothing within it — the exact shape the BUILDS predicate needed.
func countNamesOnlyCodeDeclarations(decls framework.ForeignContextDeclaration) int {
	n := 0
	for family, d := range decls {
		if family != framework.FamilyCode {
			continue
		}
		if len(d.NodeTypes) == 0 && len(d.NodeFields) == 0 &&
			len(d.MetadataKeys) == 0 && len(d.PathBasenames) == 0 {
			n++
		}
	}
	return n
}

// TestRegistration_NameIsTheGraphFamily. The registration name and the graph
// family are one string by design.
func TestRegistration_NameIsTheGraphFamily(t *testing.T) {
	assert.Equal(t, "k8s", registrationName)

	// It must not collide with a builtin graph type, or the registration would
	// be shadowed by a compiled-in collector.
	builtins := []string{"knowledge", "code", "practice", "linkage",
		"web", "pdf", "checks"}
	assert.NotContains(t, builtins, registrationName)
}

// TestRegistration_ExampleEntryRendersAsPasteableJSON. The README ships this,
// so it has to be valid and keyed by the registration name.
func TestRegistration_ExampleEntryRendersAsPasteableJSON(t *testing.T) {
	out, err := ExampleEntryJSON("linux", "/usr/local/bin/knowledge-collector-k8s")
	require.NoError(t, err)

	// THE WRAPPER IS PART OF THE SHAPE. The config file is
	// {"collectors": {"<name>": {...}}} and the client's loader decodes it with
	// DisallowUnknownFields, so a document whose top-level key is the collector's
	// name is refused with `unknown field "k8s"` — and that refusal fails the
	// operator's WHOLE file. This function rendered the unwrapped shape until the
	// client's README-loads test drove the documented block through the real
	// loader and found it.
	var doc struct {
		Collectors map[string]json.RawMessage `json:"collectors"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &doc))
	require.Contains(t, doc.Collectors, "k8s",
		"the entry is keyed by the registration name UNDER the file's own `collectors` object")

	assert.Contains(t, out, `"type": "stdio"`)
	assert.Contains(t, out, `"tool": "collect"`)
	assert.Contains(t, out, `"KUBECONFIG": "${KUBECONFIG:-}"`)
}

// TestRegistration_ExampleEntryCarriesNoCredentialValue is the security
// assertion on the shipped artifact: nothing that looks like a secret is in it.
func TestRegistration_ExampleEntryCarriesNoCredentialValue(t *testing.T) {
	out, err := ExampleEntryJSON("linux", "/usr/local/bin/knowledge-collector-k8s")
	require.NoError(t, err)
	for _, forbidden := range []string{"Bearer ", "-----BEGIN", "AKIA", "password"} {
		assert.NotContains(t, strings.ToLower(out), strings.ToLower(forbidden),
			"the shipped example must carry no credential-shaped value")
	}
}

// TestRegistration_ToolNameMatchesWhatTheCollectorServes. The entry names the
// one tool the daemon calls; an entry naming a tool the provider does not serve
// is refused at registration with a mismatch the operator has to debug.
func TestRegistration_ToolNameMatchesWhatTheCollectorServes(t *testing.T) {
	c := &k8sCollector{}
	assert.Equal(t, c.Tool().Name, ExampleEntry("linux", "/x").Tool)
	assert.NotEmpty(t, c.Tool().Description,
		"a collector meant to be installed by a human writes a description")
}

// TestRegistration_ParamsDeclaresContextOptional is R4-b's schema half, read
// FROM THE ADVERTISED SCHEMA over the real protocol.
//
// === WHY IT DOES NOT ASSERT ON GO MARSHALING ===
//
// The first version asserted that json.Marshal(Params{}) equals `{}`. That is
// true, and it is a fact about encoding/json rather than about the seam. The
// value that actually crosses is the params TYPE, which the framework infers a
// JSON Schema from and splices into the contract input document; the CLIENT
// then validates a collect's arguments against THAT document. A `context`
// landing on its `required` list would refuse every collect that names none —
// and R4 admits the currently selected context, so the refusal would be a
// broken requirement rather than a broken test.
//
// The omitempty tag IS the mechanism that keeps `context` off the required
// list, and the claim was true; what was wrong was where it sat. The marshaling
// assertion did red under the prescribed mutation, but for the wrong reason: it
// observed the tag, not the schema the tag produces, so it correlated with the
// requirement instead of asserting it. This version stands up the framework's
// own server, connects an in-memory client, and reads the tool listing the way
// a daemon would.
func TestRegistration_ParamsDeclaresContextOptional(t *testing.T) {
	schema := advertisedInputSchemaForTest(t)

	props, ok := schema["properties"].(map[string]any)
	require.True(t, ok, "the advertised input schema declares properties")
	params, ok := props["params"].(map[string]any)
	require.True(t, ok, "the contract's params property carries this collector's own schema")

	paramProps, ok := params["properties"].(map[string]any)
	require.True(t, ok, "the params schema declares its properties")
	assert.Contains(t, paramProps, "context", "the collector advertises its context parameter")

	// THE ASSERTION THE ROW IS ABOUT.
	assert.NotContains(t, params, "required",
		"context must NOT be on the advertised params required list: the client validates a "+
			"collect's arguments against this document, and a required context would refuse "+
			"every collect that names none")

	// AND THE CONTRACT'S OWN required LIST STAYS ["id"], so the splice did not
	// widen what a paramless collect must send.
	required, ok := schema["required"].([]any)
	require.True(t, ok, "the contract input schema declares a required list")
	assert.Equal(t, []any{"id"}, required)
}

// TestRegistration_AdvertisedSchemaIsReadOverTheProtocol is the known positive
// on the instrument above: it proves the document came from the framework's
// tool listing rather than from anything this test composed.
func TestRegistration_AdvertisedSchemaIsReadOverTheProtocol(t *testing.T) {
	tools := listToolsForTest(t)
	tool := toolByName(t, tools, "collect")
	assert.NotEmpty(t, tool.Description)
	assert.NotNil(t, tool.InputSchema, "the tool advertises an input schema")
}

// listToolsForTest stands up the framework's own server for this collector and
// reads its tool listing through an in-memory client session, which is the same
// call a daemon makes.
func listToolsForTest(t *testing.T) []*mcp.Tool {
	t.Helper()
	srv, err := framework.NewServer[Params](&k8sCollector{})
	require.NoError(t, err)

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := srv.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v1"}, nil)
	clientSession, err := client.Connect(t.Context(), clientTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clientSession.Close() })

	list, err := clientSession.ListTools(t.Context(), nil)
	require.NoError(t, err)
	return list.Tools
}

// advertisedInputSchemaForTest returns the served tool's input schema as a
// decoded document.
func advertisedInputSchemaForTest(t *testing.T) map[string]any {
	t.Helper()
	tools := listToolsForTest(t)
	raw, err := json.Marshal(toolByName(t, tools, "collect").InputSchema)
	require.NoError(t, err)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(raw, &doc))
	return doc
}
