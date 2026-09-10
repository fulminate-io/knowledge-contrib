// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/fulminate-io/knowledge-contrib/framework"
	"github.com/fulminate-io/knowledge-contrib/framework/frameworktest"
	"github.com/fulminate-io/knowledge-contrib/loki/internal/collect"
)

// main_test.go — the STDIO arm, and the environment the config entry supplies.
//
// THE PROVIDER UNDER TEST IS A REAL CHILD PROCESS speaking JSON-RPC over its own
// pipes, and it is THIS TEST BINARY re-executed: TestMain reads one switch
// variable and, when it is set, serves a collector through the framework's own
// ServeStdio entry point instead of running tests. That is the same entry point
// main() calls — census_test.go asserts the two are the same call — so the arm
// exercises the shipped path rather than an approximation of it, with no
// compile step and no module scaffolding in a temporary directory.
//
// STDOUT IS THE PROTOCOL STREAM: the child writes nothing to it but JSON-RPC,
// and every diagnostic goes to stderr.

const (
	// childModeEnv switches this binary from "run tests" to "be a collector".
	childModeEnv = "KN_LOKI_COLLECTOR_TEST_MODE"
	// childProbeNameEnv names the ONE environment variable the probe collector
	// answers about. The probe answers a PER-NAME lookup and never serializes
	// its whole environment: a child that dumped its environment into a result
	// the collect then admitted would write whatever the process held into a
	// graph.
	childProbeNameEnv = "KN_LOKI_COLLECTOR_TEST_PROBE_NAME"
	// childNoiseEnv makes the child write a line to STDERR before serving, so a
	// clean-stdout assertion is distinguishable from a child that wrote nothing
	// at all.
	childNoiseEnv = "KN_LOKI_COLLECTOR_TEST_STDERR_NOISE"

	childModeReal  = "real"
	childModeProbe = "probe"
)

// TestMain runs this package's tests under an EMPTY goleak allowlist, or serves
// a collector when the switch is set.
func TestMain(m *testing.M) {
	switch os.Getenv(childModeEnv) {
	case childModeReal:
		serveChild(&collect.Collector{})
	case childModeProbe:
		serveChild(&envProbeCollector{name: os.Getenv(childProbeNameEnv)})
	}
	frameworktest.VerifyNoGoroutineLeaks(m)
}

func serveChild[P any](c framework.Collector[P]) {
	if os.Getenv(childNoiseEnv) != "" {
		fmt.Fprintln(os.Stderr, "knowledge-collector-loki: a diagnostic on stderr, where diagnostics go")
	}
	if err := framework.ServeStdio(context.Background(), c); err != nil {
		fmt.Fprintf(os.Stderr, "knowledge-collector-loki: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

// envProbeParams is the probe collector's params: nothing. The name it answers
// about comes from its own environment, so the probe cannot be asked about a
// name the test did not choose before spawning it.
type envProbeParams struct{}

// envProbeCollector answers ONE per-name environment lookup as a node.
type envProbeCollector struct{ name string }

func (e *envProbeCollector) Tool() framework.ToolSpec {
	return framework.ToolSpec{Name: "probe", Description: "reports one environment lookup"}
}

func (e *envProbeCollector) Walk(context.Context, string, envProbeParams, framework.ForeignContext) (framework.Result, error) {
	value, present := os.LookupEnv(e.name)
	return framework.Result{
		Nodes: []framework.Node{{
			ID:   "probe:" + e.name,
			Type: "probe",
			Metadata: map[string]string{
				"name":    e.name,
				"present": strconv.FormatBool(present),
				"value":   value,
			},
		}},
		Complete: framework.Complete(),
	}, nil
}

// child spawns this test binary as a collector with EXACTLY the environment
// supplied, and returns a connected MCP session.
//
// THE CHILD'S ENVIRONMENT IS THE SUPPLIED MAP AND NOTHING ELSE, which is the
// config-file contract's own rule: the entry's env block is the child's whole
// environment, the daemon copies nothing from its own and adds nothing.
func child(t *testing.T, mode string, env map[string]string) *mcp.ClientSession {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("locating this test binary: %v", err)
	}
	cmd := exec.Command(self)
	cmd.Env = []string{childModeEnv + "=" + mode}
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	cmd.Stderr = os.Stderr

	// THE CHILD'S WORKING DIRECTORY IS OUTSIDE THIS WORKSPACE, for two reasons
	// that happen to want the same thing.
	//
	// It is what the daemon does: a spawned collector's working directory is a
	// temporary one, so a collector that resolved a relative path would fail in
	// production and must fail here too.
	//
	// And it makes this spawn's cache disposition true by construction rather
	// than by measurement. The go tool records what the TEST PROCESS opened; a
	// child's opens are never the test's, so anything the child read would sit
	// outside this package's cache key. This child cannot read a workspace file
	// at all: its cwd resolves nowhere near one, and the only files the
	// collector's own code opens are the paths its environment names, which no
	// case here sets to a workspace path.
	cmd.Dir = t.TempDir()

	client := mcp.NewClient(&mcp.Implementation{Name: "loki-collector-suite", Version: "v1"}, nil)
	session, err := client.Connect(t.Context(), &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connecting to the child collector: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// TestTheStdioChildAdvertisesTheToolAndAnswersACall is the stdio arm end to
// end: a real child process, the real transport, a real Loki behind it.
func TestTheStdioChildAdvertisesTheToolAndAnswersACall(t *testing.T) {
	end := time.Date(2026, 9, 7, 13, 0, 0, 0, time.UTC)
	loki := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data": map[string]any{"result": []map[string]any{{
				"stream": map[string]string{"app": "checkout", "instance": "host-3"},
				"values": [][]string{{strconv.FormatInt(end.UnixNano(), 10), "disk pressure detected"}},
			}}},
		})
	}))
	defer loki.Close()

	session := child(t, childModeReal, map[string]string{childNoiseEnv: "1"})

	list, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("listing tools: %v", err)
	}
	_ = toolByName(t, list.Tools, collect.ToolName)

	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: collect.ToolName,
		Arguments: map[string]any{
			"id": "my-collect-id",
			"params": map[string]any{
				"address": loki.URL,
				"start":   "2026-09-07T12:00:00Z",
				"end":     "2026-09-07T13:00:01Z",
			},
		},
	})
	if err != nil {
		t.Fatalf("calling the tool: %v", err)
	}
	if res.IsError {
		t.Fatalf("the tool call failed: %s", resultText(res))
	}

	envelope := structuredContent(t, res)
	nodes, _ := envelope["nodes"].([]any)
	edges, _ := envelope["edges"].([]any)
	if len(nodes) == 0 || len(edges) == 0 {
		t.Fatalf("the result carries %d nodes and %d edges", len(nodes), len(edges))
	}
	if complete, ok := envelope["walk_complete"].(bool); !ok || !complete {
		t.Fatalf("walk_complete = %v, want true", envelope["walk_complete"])
	}

	// THE FRAMING SURVIVED A RUN THAT ALSO WROTE TO STDERR, which is what makes
	// "stdout carried only JSON-RPC" a real observation rather than "the child
	// wrote nothing at all". The child wrote its diagnostic because
	// childNoiseEnv was set above, and the session still completed a handshake,
	// a listing and a call.
	types := map[string]int{}
	for _, raw := range nodes {
		n, _ := raw.(map[string]any)
		types[fmt.Sprint(n["type"])]++
	}
	for _, want := range []string{"log-template", "log-stream", "log-chunk", "log-label"} {
		if types[want] == 0 {
			t.Fatalf("the result carries no %s node; it carries %v", want, types)
		}
	}
}

// TestAWalkFailureIsAToolErrorNotAnEmptySuccess. A collect that could not read
// its source must reach the client as a refused call: an empty successful
// result would assert a walk that found nothing, and with the deletion phase
// enabled that empties the graph.
func TestAWalkFailureIsAToolErrorNotAnEmptySuccess(t *testing.T) {
	session := child(t, childModeReal, nil)
	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: collect.ToolName,
		Arguments: map[string]any{
			"id":     "my-collect-id",
			"params": map[string]any{"address": "not-a-url", "start": "2026-09-07T12:00:00Z", "end": "2026-09-07T13:00:00Z"},
		},
	})
	if err != nil {
		t.Fatalf("calling the tool: %v", err)
	}
	if !res.IsError {
		t.Fatalf("a walk against an invalid address returned a successful result: %s", resultText(res))
	}
	if !strings.Contains(resultText(res), "scheme") {
		t.Fatalf("the error does not say what was wrong with the address: %s", resultText(res))
	}
}

// TestAnEmptyCollectIDIsRefused covers the check the framework makes on this
// collector's behalf: the advertised schema can require the id to be present
// and a string, and cannot require it to be non-empty.
func TestAnEmptyCollectIDIsRefused(t *testing.T) {
	session := child(t, childModeReal, nil)
	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      collect.ToolName,
		Arguments: map[string]any{"id": "", "params": map[string]any{"address": "http://localhost:1", "start": "2026-09-07T12:00:00Z", "end": "2026-09-07T13:00:00Z"}},
	})
	if err != nil {
		t.Fatalf("calling the tool: %v", err)
	}
	if !res.IsError {
		t.Fatalf("an empty collect id was accepted: %s", resultText(res))
	}
}

// TestParamsThatDoNotSatisfyTheAdvertisedSchemaAreRefusedBeforeTheWalk. The
// generic tool handler validates the arguments against the schema this
// collector advertised, so a missing required param never reaches the walk.
func TestParamsThatDoNotSatisfyTheAdvertisedSchemaAreRefusedBeforeTheWalk(t *testing.T) {
	session := child(t, childModeReal, nil)
	for _, name := range []string{"address", "start", "end"} {
		t.Run("without "+name, func(t *testing.T) {
			params := map[string]any{"address": "http://localhost:1", "start": "2026-09-07T12:00:00Z", "end": "2026-09-07T13:00:00Z"}
			delete(params, name)
			res, err := session.CallTool(t.Context(), &mcp.CallToolParams{
				Name:      collect.ToolName,
				Arguments: map[string]any{"id": "x", "params": params},
			})
			if err != nil {
				t.Fatalf("calling the tool: %v", err)
			}
			if !res.IsError {
				t.Fatalf("params missing %q were accepted: %s", name, resultText(res))
			}
			if !strings.Contains(resultText(res), name) {
				t.Fatalf("the refusal does not name the missing param %q: %s", name, resultText(res))
			}
		})
	}
}

// TestANameInTheEntrysBlockArrivesCarryingTheBlocksValue is the arrival row.
func TestANameInTheEntrysBlockArrivesCarryingTheBlocksValue(t *testing.T) {
	const name = "LOKI_ORG_ID"
	// THE CANARY IS PLANTED IN THE PARENT and left out of the block, so this
	// one run also shows that the value the child sees is the BLOCK'S rather
	// than the one this process holds.
	t.Setenv(name, "the-parents-value")

	got := probe(t, name, map[string]string{name: "the-blocks-value"})
	if got["present"] != "true" {
		t.Fatalf("%s did not arrive in the child: %v", name, got)
	}
	if got["value"] != "the-blocks-value" {
		t.Fatalf("%s arrived carrying %q, want the block's value %q", name, got["value"], "the-blocks-value")
	}
}

// TestANameAbsentFromTheBlockIsAbsentInTheChild. Not present-and-empty, and not
// inherited: the entry's block is the child's whole environment.
func TestANameAbsentFromTheBlockIsAbsentInTheChild(t *testing.T) {
	const absent = "LOKI_BEARER_TOKEN"
	const present = "LOKI_ORG_ID"

	got := probe(t, absent, map[string]string{present: "set"})
	if got["present"] != "false" {
		t.Fatalf("%s is present in the child with value %q, though the block does not carry it", absent, got["value"])
	}
	if got["value"] != "" {
		t.Fatalf("%s arrived as %q; an absent name is absent, not empty", absent, got["value"])
	}

	// THE CONTROL, in the same shape: a name the block DOES hold arrives. Without
	// it, "absent" would not be distinguishable from a probe that reads nothing.
	got = probe(t, present, map[string]string{present: "set"})
	if got["present"] != "true" || got["value"] != "set" {
		t.Fatalf("the control name %s did not arrive: %v", present, got)
	}
}

// TestAVariablePlantedInTheParentNeverArrives is the canary, and the negative
// control the other two rows rest on. It has no counterpart anywhere else: it
// is the only row that observes what the child does NOT receive from the
// process that spawned it.
func TestAVariablePlantedInTheParentNeverArrives(t *testing.T) {
	const canary = "KN_LOKI_COLLECTOR_CANARY"
	t.Setenv(canary, "planted-in-the-parent")

	// The parent really holds it, so a false below is a property of the spawn
	// rather than of a variable that was never set.
	if v, ok := os.LookupEnv(canary); !ok || v != "planted-in-the-parent" {
		t.Fatalf("the canary is not set in this process: %q %v", v, ok)
	}

	got := probe(t, canary, map[string]string{"LOKI_ORG_ID": "something"})
	if got["present"] != "false" {
		t.Fatalf("the canary reached the child carrying %q; the child's environment is its entry's block, not the daemon's",
			got["value"])
	}
}

// probe spawns the probe collector asking about one name and returns what it
// reported.
func probe(t *testing.T, name string, block map[string]string) map[string]string {
	t.Helper()
	env := map[string]string{childProbeNameEnv: name}
	maps.Copy(env, block)
	session := child(t, childModeProbe, env)

	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "probe",
		Arguments: map[string]any{"id": "probe", "params": map[string]any{}},
	})
	if err != nil {
		t.Fatalf("calling the probe: %v", err)
	}
	if res.IsError {
		t.Fatalf("the probe failed: %s", resultText(res))
	}
	nodes, _ := structuredContent(t, res)["nodes"].([]any)
	if len(nodes) != 1 {
		t.Fatalf("the probe returned %d nodes, want 1", len(nodes))
	}
	node, _ := nodes[0].(map[string]any)
	meta, _ := node["metadata"].(map[string]any)
	out := map[string]string{}
	for k, v := range meta {
		out[k] = fmt.Sprint(v)
	}
	if out["name"] != name {
		t.Fatalf("the probe answered about %q, want %q", out["name"], name)
	}
	return out
}

func structuredContent(t *testing.T, res *mcp.CallToolResult) map[string]any {
	t.Helper()
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshaling the structured content: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("decoding the structured content: %v", err)
	}
	return out
}

func resultText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if text, ok := c.(*mcp.TextContent); ok {
			b.WriteString(text.Text)
		}
	}
	return b.String()
}

// Describe is the minimum a probe collector owes the contract: the framework
// refuses to build a server for a declaration that never said, so a fixture
// serving no declaration would fail at its own TestMain rather than in the row
// it exists for.
func (e *envProbeCollector) Describe() framework.Declaration {
	return framework.Declaration{
		Behavior: framework.BehaviorDeclaration{
			Summarizable: new(false), Embeddable: new(false), Syncable: new(true),
		},
		NodeTypes:   []string{"env-probe"},
		EdgeTypes:   []string{},
		Environment: []framework.EnvDeclaration{},
	}
}
