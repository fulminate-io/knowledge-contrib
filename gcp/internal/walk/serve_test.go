// SPDX-License-Identifier: Apache-2.0

package walk_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/fulminate-io/knowledge-contrib/framework"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/collect"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpgraph"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/walk"
)

// serve_test.go — THE COLLECTOR DRIVEN THROUGH THE FRAMEWORK, over a real stdio
// child process, once.
//
// WHY THIS EXISTS WHEN EVERY OTHER TEST CALLS Walk DIRECTLY. Calling Walk proves
// what the walk does; it proves nothing about what an operator's daemon SEES.
// Between the two sits the framework: it infers this collector's Params into a
// JSON schema, advertises that schema inside the contract's input schema, and
// VALIDATES a call's arguments against it before Walk is reached. Nothing in
// this module asserts that inference, so a params type that inferred into a
// schema refusing every real call would pass every other test here.
//
// THE CHILD IS A REAL PROCESS, re-exec'd from this test binary, because the
// thing under test includes the transport: a stray write to stdout corrupts the
// framing, and only a real child can show that.

// stubModeEnv switches this test binary into the collector it serves. A child
// process sees it set and serves; the parent never does.
const stubModeEnv = "GCP_COLLECTOR_SERVE_MODE"

// servedProject is the project the child's canned enumerations answer for. It is
// a well-formed id, because the child's job is to prove the transport and the
// schema rather than the validator, which has its own tests.
const servedProject = "proj-a-123456"

// serveAsCollector is called from TestMain in the CHILD. It serves this module's
// real collector over the framework's stdio entry point with canned
// enumerations, and never returns.
func serveAsCollector() {
	collector := walk.Collector{
		Enumerations: func(context.Context, string) ([]collect.Subcollector, func(), error) {
			return []collect.Subcollector{{
				Name: "canned",
				Run: func(context.Context, string) (gcpgraph.Result, error) {
					return gcpgraph.Result{
						Resources: []gcpgraph.Resource{{
							ID:           "https://www.googleapis.com/compute/v1/projects/p/zones/z/instances/vm",
							Name:         "vm",
							ResourceType: gcpgraph.ResourceTypeInstance,
						}},
					}, nil
				},
			}}, func() {}, nil
		},
	}
	if err := framework.ServeStdio(context.Background(), collector); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

// dial re-execs this test binary as the collector and connects to it.
func dial(t *testing.T) *mcp.ClientSession {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("resolving the test binary: %v", err)
	}
	cmd := exec.Command(self)
	cmd.Env = append(os.Environ(), stubModeEnv+"=1")
	// The child's diagnostics go to this process's stderr, so a failure in the
	// child is visible rather than swallowed by the transport.
	cmd.Stderr = os.Stderr

	client := mcp.NewClient(&mcp.Implementation{Name: "gcp-collector-suite", Version: "v1"}, nil)
	session, err := client.Connect(t.Context(), &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connecting to the collector over stdio: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// TestTheServedToolAdvertisesTheProjectParameter is the schema-inference
// observation. The framework infers Params into the advertised input schema, and
// this is the only place in this module that reads what it produced.
func TestTheServedToolAdvertisesTheProjectParameter(t *testing.T) {
	session := dial(t)

	list, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("listing the collector's tools: %v", err)
	}
	tool := toolByName(t, list.Tools, framework.DefaultToolName)
	if tool.Description == "" {
		t.Error("the served tool carries no description; an operator installing it reads that")
	}

	raw, err := json.Marshal(tool.InputSchema)
	if err != nil {
		t.Fatalf("re-encoding the advertised input schema: %v", err)
	}
	schema := string(raw)
	// The collect id and the params object are the contract's; `project` inside
	// params is THIS collector's, and it is what the inference had to produce.
	for _, want := range []string{`"id"`, `"params"`, `"project"`} {
		if !strings.Contains(schema, want) {
			t.Errorf("the advertised input schema does not carry %s: %s", want, schema)
		}
	}
	// The control: a name this collector's params do NOT have must be absent, so
	// a schema that mentioned everything would not pass.
	if strings.Contains(schema, "region") {
		t.Errorf("the advertised schema carries a parameter this collector does not take: %s", schema)
	}
}

// TestACollectOverStdioReturnsTheEnvelope drives the whole path: the SDK
// validates the arguments against the advertised schema, the framework decodes
// them, the walk runs, and the framework encodes the contract envelope.
func TestACollectOverStdioReturnsTheEnvelope(t *testing.T) {
	session := dial(t)

	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "collect",
		Arguments: map[string]any{
			"id":     "gcp-instance",
			"params": map[string]any{"project": servedProject},
		},
	})
	if err != nil {
		t.Fatalf("calling collect over stdio: %v", err)
	}
	if res.IsError {
		t.Fatalf("the collect was refused: %s", resultText(res))
	}

	var envelope struct {
		Nodes        []map[string]any `json:"nodes"`
		Edges        []map[string]any `json:"edges"`
		WalkComplete *bool            `json:"walk_complete"`
	}
	if err := json.Unmarshal(structuredJSON(t, res), &envelope); err != nil {
		t.Fatalf("decoding the envelope: %v", err)
	}
	if len(envelope.Nodes) != 1 {
		t.Errorf("the envelope carries %d nodes, want 1", len(envelope.Nodes))
	}
	// walk_complete is PRESENT and true. Present matters on its own: the
	// contract requires the key, and an encoding that omitted it on the false
	// arm would fail a collect with a missing-property error.
	if envelope.WalkComplete == nil {
		t.Fatal("the envelope carries no walk_complete key")
	}
	if !*envelope.WalkComplete {
		t.Error("a clean canned walk reported an incomplete result")
	}
}

// TestAMalformedProjectIsRefusedOverTheTransport closes the loop on the
// validator: the refusal reaches the caller as a tool error rather than as an
// empty successful collect.
func TestAMalformedProjectIsRefusedOverTheTransport(t *testing.T) {
	session := dial(t)

	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "collect",
		Arguments: map[string]any{
			"id":     "gcp-instance",
			"params": map[string]any{"project": "Not A Project"},
		},
	})
	if err != nil {
		t.Fatalf("calling collect over stdio: %v", err)
	}
	if !res.IsError {
		t.Fatal("a malformed project id was collected rather than refused")
	}
	if !strings.Contains(resultText(res), "lowercase") {
		t.Errorf("the refusal did not survive the transport intact: %s", resultText(res))
	}
}

// resultText flattens a call result's text content.
func resultText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, content := range res.Content {
		if text, ok := content.(*mcp.TextContent); ok {
			b.WriteString(text.Text)
		}
	}
	return b.String()
}

// structuredJSON is the result's structured content, which is the envelope.
func structuredJSON(t *testing.T, res *mcp.CallToolResult) []byte {
	t.Helper()
	if res.StructuredContent == nil {
		// The SDK returns the payload as text when a caller declares no output
		// type, so the text is the envelope.
		return []byte(resultText(res))
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("re-encoding the structured result: %v", err)
	}
	return raw
}
