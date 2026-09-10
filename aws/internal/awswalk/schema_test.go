// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// schema_test.go — THE ADVERTISED INPUT SCHEMA, which is what the client
// validates a collect's arguments against BEFORE it sends them.
//
// TWO PROPERTIES DECIDE WHETHER A COLLECT REACHES THIS COLLECTOR AT ALL, and both
// fail closed in a way no walk test would see:
//
//  1. The selectors must be NAMED in properties.params, or a collect carrying one
//     is refused by the client for declaring a property the schema does not.
//  2. Neither the selectors nor `params` itself may be REQUIRED. The client omits
//     the params key entirely when a collect carries none, so a required entry
//     refuses every paramless collect — which is the ordinary case.
//
// THE SUBJECT IS THE DOCUMENT THE FRAMEWORK ADVERTISES, not this package's Params
// type: the splice is the framework's, and asserting on the Go type would test
// the wrong artifact.

// advertisedInput returns the input schema the framework advertises for this
// collector, decoded.
func advertisedInput(t *testing.T) map[string]any {
	t.Helper()
	srv, err := framework.NewServer[Params](New())
	if err != nil {
		t.Fatalf("the framework must build a server for this collector: %v", err)
	}
	tools := listTools(t, srv)
	raw, err := json.Marshal(toolByName(t, tools, ToolName).InputSchema)
	if err != nil {
		t.Fatalf("marshal the advertised input schema: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decode the advertised input schema: %v", err)
	}
	return doc
}

// listTools drives the MCP server's own tool listing in process.
func listTools(t *testing.T, srv *mcp.Server) []*mcp.Tool {
	t.Helper()
	ctx := context.Background()
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := srv.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connect the server: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client := mcp.NewClient(&mcp.Implementation{Name: "schema-test", Version: "v1"}, nil)
	session, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect the client: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	res, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	return res.Tools
}

func TestSchema_ParamsNamesEverySelector(t *testing.T) {
	doc := advertisedInput(t)
	props, ok := doc["properties"].(map[string]any)
	if !ok {
		t.Fatal("the advertised input schema declares no properties object")
	}
	params, ok := props["params"].(map[string]any)
	if !ok {
		t.Fatal("the advertised input schema declares no params property; the framework's splice did not happen")
	}
	paramProps, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatal("the params sub-schema declares no properties; a collect naming a selector would be refused " +
			"by the client for declaring a property the schema does not")
	}
	for _, want := range []string{"region", "account"} {
		if _, ok := paramProps[want]; !ok {
			t.Errorf("the params sub-schema does not name the %q selector, so a collect carrying it is refused "+
				"before it reaches this collector", want)
		}
	}
}

func TestSchema_NothingInParamsIsRequired(t *testing.T) {
	doc := advertisedInput(t)

	// `params` ITSELF must not be required: the client omits the key entirely
	// when a collect carries none.
	if req, ok := doc["required"].([]any); ok {
		for _, r := range req {
			if r == "params" {
				t.Error("the advertised input schema marks `params` REQUIRED; the client omits that key when a " +
					"collect supplies no params, so every paramless collect would be refused before it was sent")
			}
		}
	}
	props := doc["properties"].(map[string]any)
	params, ok := props["params"].(map[string]any)
	if !ok {
		t.Fatal("no params property")
	}
	if req, ok := params["required"].([]any); ok && len(req) > 0 {
		t.Errorf("the params sub-schema marks %v required; a collect that supplies no params carries none of "+
			"them, so a required key refuses the ordinary case", req)
	}
}

// TestSchema_TheIDIsStillRequired is the KNOWN POSITIVE for the two arms above:
// without it they are satisfied by a schema with no required list at all, which
// would be a different defect.
func TestSchema_TheIDIsStillRequired(t *testing.T) {
	doc := advertisedInput(t)
	req, ok := doc["required"].([]any)
	if !ok || len(req) == 0 {
		t.Fatal("the advertised input schema declares no required list; the contract requires `id`, which names " +
			"the graph instance the result lands in")
	}
	found := false
	for _, r := range req {
		if r == "id" {
			found = true
		}
	}
	if !found {
		t.Errorf("the advertised input schema does not require `id`; got required=%v", req)
	}
}

// TestSchema_TheServedToolIsNamedForThisCollector pins the name a config entry's
// `tool` field must carry. An entry naming a tool the provider does not list is
// refused at install, so a rename here is a change to every installation.
func TestSchema_TheServedToolIsNamedForThisCollector(t *testing.T) {
	srv, err := framework.NewServer[Params](New())
	if err != nil {
		t.Fatalf("build the server: %v", err)
	}
	tool := toolByName(t, listTools(t, srv), ToolName)
	if tool.Description == "" {
		t.Error("the served tool carries no description; an operator installing it by hand reads that string")
	}
}
