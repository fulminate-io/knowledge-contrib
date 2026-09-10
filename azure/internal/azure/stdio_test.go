// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// stdio_test.go — the collector AS A PROCESS: a real child serving MCP over
// stdin and stdout, driven by a real client.
//
// WHY NOT IN PROCESS. What an operator installs is a binary a daemon spawns and
// talks to over a pipe, and three of the properties that matter are properties
// of that arrangement rather than of any function: the tool is advertised at
// all, its input schema is the one the client validates against, and a call
// that violates it is refused BEFORE the walk runs. A test calling Walk
// directly observes none of them.

// dialChild spawns the test binary in its serving mode and returns a connected
// MCP session.
func dialChild(t *testing.T) *mcp.ClientSession {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	cmd := exec.CommandContext(ctx, testBinary(t))
	cmd.Env = []string{childModeEnv + "=" + childModeServe}
	cmd.Stderr = os.Stderr
	// THE CHILD RUNS OUTSIDE THE WORKSPACE. It is not isolation for its own
	// sake: the workspace's cache-blindness census asks whether a spawned
	// child reads anything a cache key should have covered, and a child whose
	// working directory is a scratch tree cannot resolve a workspace-relative
	// path at all. A child that needed one would fail here rather than pass
	// quietly against bytes no key sees.
	cmd.Dir = t.TempDir()

	client := mcp.NewClient(&mcp.Implementation{Name: "azure-collector-test", Version: "v1"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connecting to the collector over stdio: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// TestStdio_AdvertisesOneCollectToolWithTheContractsInputShape.
func TestStdio_AdvertisesOneCollectToolWithTheContractsInputShape(t *testing.T) {
	session := dialChild(t)

	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("listing tools: %v", err)
	}
	tool := toolByName(t, tools.Tools, framework.DefaultToolName)
	if tool.Description == "" {
		t.Error("the tool has no description, so an operator reading a tool listing learns nothing")
	}

	schema := decodeSchema(t, tool.InputSchema)
	required, _ := schema["required"].([]any)
	if len(required) != 1 || required[0] != "id" {
		t.Errorf("the advertised input requires %v; the contract requires the collect id alone, and any other required key refuses every collect that omits it", required)
	}
	props, _ := schema["properties"].(map[string]any)
	if _, ok := props["id"]; !ok {
		t.Error("the advertised input declares no id, which is what names the subscription")
	}
	params, ok := props["params"].(map[string]any)
	if !ok {
		t.Fatalf("the advertised input declares no params object: %v", props)
	}
	if params["type"] != "object" {
		t.Errorf("params is advertised as %v rather than an object", params["type"])
	}
}

// TestStdio_TheParamsSchemaDeclaresEveryKeyAndNoCredential. The keys are this
// module's own half of the input; the absence of a credential property is the
// property that matters, because a credential key here would invite an operator
// to put a secret in a collect call.
func TestStdio_TheParamsSchemaDeclaresEveryKeyAndNoCredential(t *testing.T) {
	session := dialChild(t)
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("listing tools: %v", err)
	}
	schema := decodeSchema(t, tools.Tools[0].InputSchema)
	props, _ := schema["properties"].(map[string]any)
	params, _ := props["params"].(map[string]any)
	paramProps, _ := params["properties"].(map[string]any)

	for _, want := range []string{"subscription_id", "max_concurrency"} {
		if _, ok := paramProps[want]; !ok {
			t.Errorf("the params schema does not declare %q, which the walk reads", want)
		}
	}
	if req, ok := params["required"]; ok {
		if list, _ := req.([]any); len(list) > 0 {
			t.Errorf("the params schema marks %v required, which refuses every collect that omits them", list)
		}
	}
	for key := range paramProps {
		for _, credentialish := range []string{"secret", "password", "token", "credential", "key", "certificate"} {
			if strings.Contains(strings.ToLower(key), credentialish) {
				t.Errorf("the params schema declares %q, which reads as a credential; credentials reach this collector "+
					"only through its environment", key)
			}
		}
	}
}

// TestStdio_CollectAnswersOverTheProtocol. The end-to-end arm: a call over the
// pipe reaches the walk and its result comes back as the contract envelope.
func TestStdio_CollectAnswersOverTheProtocol(t *testing.T) {
	session := dialChild(t)

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      framework.DefaultToolName,
		Arguments: map[string]any{"id": "sub-1"},
	})
	if err != nil {
		t.Fatalf("calling collect: %v", err)
	}
	if res.IsError {
		t.Fatalf("the collect was refused: %v", res.Content)
	}

	envelope := decodeEnvelope(t, res)
	if envelope.WalkComplete != true {
		t.Error("a walk with no failure did not assert completeness over the wire")
	}
	if len(envelope.Nodes) != 1 || envelope.Nodes[0].ID != vmID {
		t.Errorf("the walk's node did not survive the wire: %v", envelope.Nodes)
	}
	if envelope.Nodes[0].Type != nodeTypeCloudResource {
		t.Errorf("the node's type crossed the wire as %q", envelope.Nodes[0].Type)
	}
	if envelope.Nodes[0].Metadata[metaResourceType] != rtVM {
		t.Errorf("the Azure resource type did not survive as metadata: %v", envelope.Nodes[0].Metadata)
	}
	if len(envelope.Edges) != 1 || envelope.Edges[0].Type != edgeUsesSubnet {
		t.Errorf("the walk's edge did not survive the wire: %v", envelope.Edges)
	}
}

// TestStdio_ACollectWithNoParamsIsAccepted. The client omits params entirely
// when a collect names none, so a provider that required them would refuse the
// ordinary collect before it was sent.
func TestStdio_ACollectWithNoParamsIsAccepted(t *testing.T) {
	session := dialChild(t)
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      framework.DefaultToolName,
		Arguments: map[string]any{"id": "sub-1"},
	})
	if err != nil {
		t.Fatalf("calling collect with no params: %v", err)
	}
	if res.IsError {
		t.Fatalf("a collect carrying no params was refused: %v", res.Content)
	}
	// And one carrying params the schema admits reaches the walk too.
	withParams, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      framework.DefaultToolName,
		Arguments: map[string]any{"id": "sub-1", "params": map[string]any{"max_concurrency": 2}},
	})
	if err != nil {
		t.Fatalf("calling collect with admitted params: %v", err)
	}
	if withParams.IsError {
		t.Fatalf("a collect carrying admitted params was refused: %v", withParams.Content)
	}
}

// TestStdio_AnEmptyCollectIdIsRefusedBeforeTheWalk. An empty id names no graph
// instance; the contract's own schema cannot express non-empty, so the refusal
// is the provider's.
func TestStdio_AnEmptyCollectIdIsRefusedBeforeTheWalk(t *testing.T) {
	session := dialChild(t)
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      framework.DefaultToolName,
		Arguments: map[string]any{"id": ""},
	})
	if err != nil {
		// A transport error is also a refusal; either shape is acceptable as
		// long as the collect did not succeed.
		return
	}
	if !res.IsError {
		t.Fatalf("a collect naming no graph instance succeeded: %v", res.Content)
	}
}

// TestStdio_AToolTheProviderDoesNotAdvertiseIsRefused. It is the arm an
// operator hits when their config entry's `tool` field is wrong, and the
// refusal has to be legible rather than a hang.
func TestStdio_AToolTheProviderDoesNotAdvertiseIsRefused(t *testing.T) {
	session := dialChild(t)
	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      "collect_everything",
		Arguments: map[string]any{"id": "sub-1"},
	})
	if err != nil {
		return
	}
	if !res.IsError {
		t.Fatal("a tool this provider does not advertise was served")
	}
}

// decodeSchema renders an advertised schema as a plain document, whatever
// concrete shape the SDK carried it as.
func decodeSchema(t *testing.T, schema any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("re-encoding the advertised schema: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decoding the advertised schema: %v", err)
	}
	return doc
}

// contractEnvelope is the contract's output shape, decoded from a call result.
type contractEnvelope struct {
	Nodes        []framework.Node `json:"nodes"`
	Edges        []framework.Edge `json:"edges"`
	WalkComplete bool             `json:"walk_complete"`
}

func decodeEnvelope(t *testing.T, res *mcp.CallToolResult) contractEnvelope {
	t.Helper()
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("re-encoding the call's structured content: %v", err)
	}
	var envelope contractEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("decoding the contract envelope: %v", err)
	}
	return envelope
}
