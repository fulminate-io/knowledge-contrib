// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// spawn_test.go — the arm that drives a REAL stdio child. Both sides are real:
// the child runs the framework's shipped ServeStdio entry point over its own
// pipes, and the parent is the SDK's client.
//
// WHAT IT PROVES THAT AN IN-PROCESS ARM CANNOT: that the schemas this collector
// advertises reach a caller over the wire, that the tool answers a call carrying
// an id, and that nothing this module writes corrupts the JSON-RPC framing —
// which is the failure that surfaces as an opaque handshake error with nothing
// pointing at its cause.

// dialSpawnedCollector spawns this test binary as a collector and connects.
func dialSpawnedCollector(t *testing.T, mode string) *mcp.ClientSession {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("resolving the test binary: %v", err)
	}
	cmd := exec.Command(self)
	cmd.Env = append(os.Environ(), spawnModeEnv+"="+mode)
	cmd.Stderr = os.Stderr

	// THE CHILD RUNS OUTSIDE THE WORKSPACE, and that is a control rather than
	// tidiness. The go tool keys a package's cached result on what the TEST
	// PROCESS opened; a child's opens are never the test's, so anything this
	// child read from the workspace would be outside the cache key and the
	// package would be cacheable against a subject it never read. Running it
	// from a temp directory makes that impossible to do by accident: a
	// workspace-relative read here fails outright rather than passing quietly.
	cmd.Dir = t.TempDir()

	client := mcp.NewClient(&mcp.Implementation{Name: "stackdriver-suite", Version: "v1"}, nil)
	session, err := client.Connect(t.Context(), &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connecting to the spawned collector: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// TestTheSpawnedProviderAdvertisesTheFourNamedInputs is the input-naming row.
// The framework advertises the CONTRACT's top level and splices in the schema
// inferred from this module's own params type, so the names inside `params` are
// this module's to get right — and a caller naming an input the sub-schema does
// not declare is refused before the call rather than ignored.
func TestTheSpawnedProviderAdvertisesTheFourNamedInputs(t *testing.T) {
	session := dialSpawnedCollector(t, spawnRecorded)
	tools, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("listing tools: %v", err)
	}
	tool := toolByName(t, tools.Tools, toolName)

	props := paramsProperties(t, tool.InputSchema)
	// The ticket's three inputs plus the entry bound the result cap forces.
	for _, want := range []string{"project", "filter", "start", "end", "max_entries"} {
		if _, ok := props[want]; !ok {
			t.Errorf("the params sub-schema declares no %q; it declares %v", want, sortedNames(props))
		}
	}
}

// TestTheAdvertisedTopLevelStaysTheContractsOwn is the paired assertion: this
// module's inputs ride INSIDE params rather than beside it. A provider that
// pushed them to the top level would tighten the contract's own required list,
// and every collect the client sends without that key would be refused before
// the call as a client bug.
func TestTheAdvertisedTopLevelStaysTheContractsOwn(t *testing.T) {
	session := dialSpawnedCollector(t, spawnRecorded)
	tools, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("listing tools: %v", err)
	}
	doc := schemaDocument(t, tools.Tools[0].InputSchema)

	required, _ := doc["required"].([]any)
	if len(required) != 1 || required[0] != "id" {
		t.Errorf("the advertised top-level required list is %v, want exactly [id]", required)
	}

	// THE THREE THE CONTRACT ITSELF DECLARES. `context` is the client-filled
	// foreign-graph block, which is the contract's own property rather than this
	// collector's: the framework advertises the contract file with only `params`
	// spliced, so a fourth name here would be this module widening the top level.
	contractTop := map[string]bool{"id": true, "params": true, "context": true}
	top, _ := doc["properties"].(map[string]any)
	for name := range top {
		if !contractTop[name] {
			t.Errorf("the advertised top level declares %q, which is not the contract's", name)
		}
	}
	for name := range contractTop {
		if _, ok := top[name]; !ok {
			t.Errorf("the advertised top level does not declare the contract's %q", name)
		}
	}
}

// TestTheSpawnedProviderAnswersACollectOverStdio is the whole served path
// through a real child: connect, call, decode the envelope.
func TestTheSpawnedProviderAnswersACollectOverStdio(t *testing.T) {
	session := dialSpawnedCollector(t, spawnRecorded)
	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      toolName,
		Arguments: map[string]any{"id": "collect-1", "params": map[string]any{"project": "fulminate-services"}},
	})
	if err != nil {
		t.Fatalf("calling the tool: %v", err)
	}
	if res.IsError {
		t.Fatalf("the call returned an error result: %+v", res.Content)
	}

	var out struct {
		Nodes []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"nodes"`
		Edges        []map[string]any `json:"edges"`
		WalkComplete *bool            `json:"walk_complete"`
	}
	if err := json.Unmarshal(mustMarshal(t, res.StructuredContent), &out); err != nil {
		t.Fatalf("decoding the envelope: %v", err)
	}
	if out.WalkComplete == nil {
		t.Fatalf("the envelope carries no walk_complete")
	}
	if !*out.WalkComplete {
		t.Errorf("the recorded walk asserted an incomplete read")
	}
	if len(out.Nodes) == 0 || len(out.Edges) == 0 {
		t.Fatalf("the envelope carries %d nodes and %d edges", len(out.Nodes), len(out.Edges))
	}
	for i, n := range out.Nodes {
		if n.ID == "" || n.Type == "" {
			t.Errorf("node %d crossed the wire with id %q and type %q", i, n.ID, n.Type)
		}
	}
}

// TestAnEmptyWalkCrossesTheWireAsArraysNotNull is the empty-result cell over the
// real transport, where a nil slice would marshal to null and fail the
// advertised output schema's array type.
func TestAnEmptyWalkCrossesTheWireAsArraysNotNull(t *testing.T) {
	session := dialSpawnedCollector(t, spawnEmpty)
	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      toolName,
		Arguments: map[string]any{"id": "collect-1", "params": map[string]any{"project": "p"}},
	})
	if err != nil {
		t.Fatalf("calling the tool: %v", err)
	}
	if res.IsError {
		t.Fatalf("an empty walk returned an error result: %+v", res.Content)
	}
	body := string(mustMarshal(t, res.StructuredContent))
	for _, forbidden := range []string{`"nodes":null`, `"edges":null`} {
		if contains(body, forbidden) {
			t.Errorf("the empty envelope carries %s: %s", forbidden, body)
		}
	}
}

// TestACollectOmittingTheParamsKeyEntirelyReachesTheWalk is the absent-params
// wire shape, which is a different input class from an EMPTY params object.
//
// THE CLIENT OMITS `params` ENTIRELY when a collect carries none, so this is the
// shape a paramless collect actually sends. It must pass schema validation — the
// advertised top-level required list is exactly [id] — and reach the walk, which
// then refuses on its own terms because this collector needs a project. A
// provider that had pushed its inputs to the top level would fail here instead,
// before the call, and the failure would look like a client bug.
func TestACollectOmittingTheParamsKeyEntirelyReachesTheWalk(t *testing.T) {
	session := dialSpawnedCollector(t, spawnRecorded)
	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      toolName,
		Arguments: map[string]any{"id": "collect-1"},
	})
	if err != nil {
		t.Fatalf("a collect with no params key was refused before the call: %v", err)
	}
	if !res.IsError {
		t.Fatalf("a collect with no project succeeded: %+v", res.StructuredContent)
	}
	body := renderContent(t, res)
	if !contains(body, "project") {
		t.Errorf("the refusal does not name the missing input, so it is a schema refusal "+
			"rather than the walk's own: %s", body)
	}
}

// TestTheBoundsThreeInputsBehaveDifferentlyOverTheWire drives the absent, null,
// zero and positive spellings of max_entries through a real child, because the
// distinction between them is a property of the DECODE and a struct built in
// process would not exercise it.
//
// THE NULL SPELLING EXISTS BECAUSE THE FIELD IS A POINTER: the inferred schema
// declares max_entries as integer-or-null, so a caller may send null and the
// decode gives nil. That is read as ABSENT, which is the coherent reading — null
// is JSON's own spelling of "no value", not a number of entries — and it is
// asserted here rather than left as an accident of the type.
//
// The union does not reach the client's registration gate: that comparator
// descends only into properties the CONTRACT declares, and the contract declares
// params as an object without naming what is inside it.
func TestTheBoundsThreeInputsBehaveDifferentlyOverTheWire(t *testing.T) {
	session := dialSpawnedCollector(t, spawnRecorded)

	for _, tc := range []struct {
		name      string
		params    map[string]any
		wantError bool
	}{
		{"absent is unbounded", map[string]any{"project": "p"}, false},
		{"null reads as absent", map[string]any{"project": "p", "max_entries": nil}, false},
		{"an explicit zero is refused", map[string]any{"project": "p", "max_entries": 0}, true},
		{"a negative is refused", map[string]any{"project": "p", "max_entries": -1}, true},
		{"a positive is honored", map[string]any{"project": "p", "max_entries": 3}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := session.CallTool(t.Context(), &mcp.CallToolParams{
				Name:      toolName,
				Arguments: map[string]any{"id": "collect-1", "params": tc.params},
			})
			if err != nil {
				if !tc.wantError {
					t.Fatalf("refused before the call: %v", err)
				}
				return
			}
			if res.IsError != tc.wantError {
				t.Fatalf("IsError = %v, want %v: %s", res.IsError, tc.wantError, renderContent(t, res))
			}
			if tc.wantError && !contains(renderContent(t, res), "max_entries") {
				t.Errorf("the refusal does not name the field: %s", renderContent(t, res))
			}
		})
	}
}

// TestABadParamRefusalCrossesTheWireAsAToolError is the failure arm over the
// real transport: a refused collect is an error result rather than a successful
// empty one, because the client treats the two completely differently.
func TestABadParamRefusalCrossesTheWireAsAToolError(t *testing.T) {
	session := dialSpawnedCollector(t, spawnRecorded)
	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      toolName,
		Arguments: map[string]any{"id": "collect-1", "params": map[string]any{"project": ""}},
	})
	if err != nil {
		// A transport-level error is also a refusal; either way it is not a
		// successful empty collect.
		return
	}
	if !res.IsError {
		t.Fatalf("a collect with an empty project succeeded: %+v", res.StructuredContent)
	}
}

// paramsProperties returns the property names inside the advertised params
// sub-schema.
func paramsProperties(t *testing.T, schema any) map[string]any {
	t.Helper()
	doc := schemaDocument(t, schema)
	props, ok := doc["properties"].(map[string]any)
	if !ok {
		t.Fatalf("the advertised input schema declares no properties: %v", doc)
	}
	paramsSchema, ok := props["params"].(map[string]any)
	if !ok {
		t.Fatalf("the advertised input schema declares no params property: %v", props)
	}
	inner, ok := paramsSchema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("the params sub-schema declares no properties: %v", paramsSchema)
	}
	return inner
}

// schemaDocument decodes an advertised schema into a plain document.
func schemaDocument(t *testing.T, schema any) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(mustMarshal(t, schema), &doc); err != nil {
		t.Fatalf("decoding the advertised schema: %v", err)
	}
	return doc
}

// renderContent flattens a tool result's content blocks into one string, which
// is where an error result's message lands.
func renderContent(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	return string(mustMarshal(t, res.Content))
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshaling: %v", err)
	}
	return b
}

func sortedNames(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
