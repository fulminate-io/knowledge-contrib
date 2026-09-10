// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/fulminate-io/knowledge-contrib/framework"
	"github.com/fulminate-io/knowledge-contrib/framework/frameworktest"
)

// schema_test.go — THE PARAMS SUB-SCHEMA THIS COLLECTOR ADVERTISES.
//
// It is read off the ADVERTISED DOCUMENT, through a real MCP session, rather
// than off the Go struct. The framework infers the schema from the params type,
// so what this collector means to advertise and what it advertises are two
// different things until one of them is read; a row that checked the struct's
// tags would pass on an inference that dropped a field.

// advertisedParams returns the `params` sub-schema this collector advertises,
// decoded from the tool the framework builds.
func advertisedParams(t *testing.T) map[string]any {
	t.Helper()

	// THE SCHEMA IS READ THROUGH THE PROTOCOL, from a real MCP session against
	// a real server serving this collector. Reading it off the Go struct would
	// assert what this collector MEANS to advertise; a tool listing is what the
	// knowledge client actually receives and validates a collect against.
	handler, srv, err := framework.NewHTTPHandler[Params](&Collector{})
	if err != nil {
		t.Fatalf("NewHTTPHandler: %v", err)
	}
	defer frameworktest.DrainServerSessions(srv)

	httpServer := httptest.NewServer(handler)
	defer httpServer.Close()

	client := mcp.NewClient(&mcp.Implementation{Name: "loki-collector-suite", Version: "v1"}, nil)
	session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: httpServer.URL}, nil)
	if err != nil {
		t.Fatalf("connecting to the collector: %v", err)
	}
	defer session.Close()

	list, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("listing tools: %v", err)
	}
	tool := toolByName(t, list.Tools, ToolName)

	raw, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatalf("marshaling the advertised input schema: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decoding the advertised input schema: %v", err)
	}
	props, ok := doc["properties"].(map[string]any)
	if !ok {
		t.Fatalf("the advertised input schema declares no properties: %s", raw)
	}
	params, ok := props["params"].(map[string]any)
	if !ok {
		t.Fatalf("the advertised input schema declares no params object: %s", raw)
	}
	return params
}

func paramProperty(t *testing.T, params map[string]any, name string) map[string]any {
	t.Helper()
	props, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatalf("the params schema declares no properties: %v", params)
	}
	prop, ok := props[name].(map[string]any)
	if !ok {
		t.Fatalf("the params schema declares no %q property; it declares %v", name, keysOf(props))
	}
	return prop
}

func requiredParams(t *testing.T, params map[string]any) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	raw, ok := params["required"]
	if !ok {
		return out
	}
	list, ok := raw.([]any)
	if !ok {
		t.Fatalf("the params schema's required list is %T, want an array", raw)
	}
	for _, v := range list {
		out[v.(string)] = true
	}
	return out
}

func keysOf(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestTheAdvertisedParamsDeclareTheAddress is the address half of the ticket's
// three named inputs.
func TestTheAdvertisedParamsDeclareTheAddress(t *testing.T) {
	params := advertisedParams(t)
	prop := paramProperty(t, params, "address")
	if prop["type"] != "string" {
		t.Fatalf("address is typed %v, want string", prop["type"])
	}
	if !requiredParams(t, params)["address"] {
		t.Fatalf("address is not required; the required list is %v", params["required"])
	}
	if desc, _ := prop["description"].(string); desc == "" {
		t.Fatal("address carries no description; the description is what an operator reads in a tool listing")
	}
}

// TestTheAdvertisedParamsDeclareTheSelector is the LogQL-selector half.
func TestTheAdvertisedParamsDeclareTheSelector(t *testing.T) {
	params := advertisedParams(t)
	prop := paramProperty(t, params, "selector")
	if prop["type"] != "string" {
		t.Fatalf("selector is typed %v, want string", prop["type"])
	}
	// It is deliberately OPTIONAL: an empty selector falls back to the
	// structured fields and then to every namespaced stream, which is the
	// behavior the built-in adapter has.
	if requiredParams(t, params)["selector"] {
		t.Fatal("selector is required; an empty selector has a defined meaning, so it must be optional")
	}
}

// TestTheAdvertisedParamsDeclareBothTimeWindowBounds is the time-range half.
// It asserts BOTH bounds: a row that checked one would leave the other to the
// inference this whole file exists to distrust.
func TestTheAdvertisedParamsDeclareBothTimeWindowBounds(t *testing.T) {
	params := advertisedParams(t)
	required := requiredParams(t, params)
	for _, name := range []string{"start", "end"} {
		prop := paramProperty(t, params, name)
		if prop["type"] != "string" {
			t.Fatalf("%s is typed %v, want string", name, prop["type"])
		}
		if !required[name] {
			t.Fatalf("%s is not required; the required list is %v", name, params["required"])
		}
	}
}

// TestTheAdvertisedParamsDeclareEveryFilter covers the remaining properties, so
// a field silently dropped from the inference is a failure rather than a
// parameter an operator sets and finds ignored.
func TestTheAdvertisedParamsDeclareEveryFilter(t *testing.T) {
	params := advertisedParams(t)
	for _, name := range []string{"source", "field_filters", "text_filter", "severity_min", "raw_query"} {
		prop := paramProperty(t, params, name)
		if prop["type"] == nil {
			t.Fatalf("%s carries no type", name)
		}
	}
	if params["type"] != "object" {
		t.Fatalf("the params schema is typed %v, want object", params["type"])
	}
}
