// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/fulminate-io/knowledge-contrib/framework"
	"github.com/fulminate-io/knowledge-contrib/framework/frameworktest"
)

// main_test.go — the goroutine-leak gate, and the switch that turns this test
// binary into a real stdio collector.
//
// THE PROVIDER UNDER TEST IS THIS BINARY, RE-EXECED. What the stdio arm has to
// exercise is a real child process speaking JSON-RPC over its own pipes with an
// environment this test controls, and a child that is this binary needs no
// compile step and no temporary module. TestMain checks one switch variable and,
// when it is set, serves a collector instead of running tests.
//
// THE SWITCH VARIABLE IS ITSELF ON THE CHILD'S ENVIRONMENT, which is the
// environment contract demonstrating itself: the child receives exactly the
// names its entry declares, so a switch the entry did not carry would not reach
// the child and the re-exec would silently run the test suite again.
//
// NOTHING BUT JSON-RPC REACHES STDOUT. Stdout is the protocol stream on stdio;
// a stray print corrupts the framing and surfaces as an opaque handshake
// failure rather than as the print it was. The one diagnostic below goes to
// stderr.

// The switch variable and its two modes.
const (
	testModeEnv = "KN_CLOUDWATCH_TEST_MODE"
	// modeServe serves the REAL collector, so the advertised schemas and the
	// refusal arms are read off the shipped code path.
	modeServe = "serve"
	// modeEnvProbe serves a collector that reports, per name, whether that name
	// is present in the child's own environment.
	modeEnvProbe = "envprobe"
	// modeTransport serves a collector that walks a caller-supplied endpoint
	// through a REAL SDK client, so a transport failure is produced by the
	// transport rather than simulated.
	modeTransport = "transport"
	// modeRoundTrip serves the SHIPPED collector against recorded pages, so a
	// collect can be driven end to end over the wire with no credential and no
	// network. It is the only mode whose subject is the production Walk.
	modeRoundTrip = "roundtrip"
	// modeCorrelate serves the SHIPPED collector against pages carrying error
	// bursts in TWO services, which is what a correlation needs.
	modeCorrelate = "correlate"
)

// TestMain installs the goroutine-leak gate with an EMPTY allowlist, inherited
// from the framework's own helper so this module takes the gate in one line.
//
// AN ENTRY ADDED TO THAT ALLOWLIST LATER MUST NAME ITS GOROUTINE and say why
// its lifetime legitimately exceeds the test that started it. This package
// needs none: every spawned child is closed by the session cleanup.
func TestMain(m *testing.M) {
	switch os.Getenv(testModeEnv) {
	case modeServe:
		serveOverStdio(New())
	case modeEnvProbe:
		serveOverStdio(&envProbeCollector{})
	case modeTransport:
		serveOverStdio(&transportProbeCollector{})
	case modeRoundTrip:
		serveOverStdio(roundTripCollector())
	case modeCorrelate:
		serveOverStdio(recordedCollector(correlationEvents()))
	default:
		frameworktest.VerifyNoGoroutineLeaks(m)
	}
}

// serveOverStdio runs a collector on the shipped entry point and never returns.
func serveOverStdio[P any](c framework.Collector[P]) {
	if err := framework.ServeStdio(context.Background(), c); err != nil {
		fmt.Fprintf(os.Stderr, "cloudwatch test provider: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

// envProbeParams names the environment variables the probe should report on.
type envProbeParams struct {
	Names []string `json:"names"`
}

// envProbeCollector answers PER-NAME lookups and never serializes its whole
// environment.
//
// THAT RESTRAINT IS THE POINT. A probe that dumped its environment would put
// every value an operator supplied into a test log, and it would answer
// questions the test did not ask — including whether a name the test never
// declared happens to be present, which is exactly the assertion the negative
// control has to make for itself.
type envProbeCollector struct{}

func (e *envProbeCollector) Tool() framework.ToolSpec {
	return framework.ToolSpec{Name: toolName, Description: "environment probe"}
}

// Walk emits one node per requested name: its id is the name, its status says
// whether the name was present, and its content is the value.
//
// ABSENT AND EMPTY ARE DISTINGUISHED, which is the whole question: an absent
// name and a name set to the empty string both read as "" through the ordinary
// lookup, and the contract's claim is about absence.
func (e *envProbeCollector) Walk(
	_ context.Context, _ string, params envProbeParams, _ framework.ForeignContext,
) (framework.Result, error) {
	nodes := make([]framework.Node, 0, len(params.Names))
	for _, name := range params.Names {
		value, present := os.LookupEnv(name)
		status := "absent"
		if present {
			status = "present"
		}
		nodes = append(nodes, framework.Node{ID: name, Type: "env", Status: status, Content: value})
	}
	return framework.Result{Nodes: nodes, Complete: framework.Complete()}, nil
}

// stdioProvider is a live connection to a spawned child collector.
type stdioProvider struct {
	session *mcp.ClientSession
}

// dialStdioProvider spawns this test binary as a stdio collector with the
// DEFAULT environment: the switch variable alone, plus whatever a given test
// adds. Both sides are real — the child runs the framework's shipped entry
// point and the parent is the SDK's own client.
func dialStdioProvider(t *testing.T, mode string) *stdioProvider {
	t.Helper()
	return dialStdioProviderWithEnv(t, mode, nil)
}

// dialStdioProviderWithEnv spawns the child with EXACTLY the given environment
// block plus the switch variable.
//
// THE CHILD'S ENVIRONMENT IS SET WHOLE, never appended to the parent's, because
// that is what an installed collector receives: the configuration entry's env
// block IS the child's complete environment, and a name the block does not
// carry is absent whatever the spawning process holds.
func dialStdioProviderWithEnv(t *testing.T, mode string, block map[string]string) *stdioProvider {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("resolving the test binary: %v", err)
	}
	cmd := exec.Command(self)
	env := []string{testModeEnv + "=" + mode}
	for name, value := range block {
		env = append(env, name+"="+value)
	}
	cmd.Env = env
	cmd.Stderr = os.Stderr
	// THE CHILD RUNS OUTSIDE THE WORKSPACE, which is a control rather than
	// tidiness: the go tool records what the TEST PROCESS opened, never what a
	// child opened, so anything a child read under this workspace would sit
	// outside this package's cache key. Running it from a scratch directory
	// makes any workspace-relative read fail, so the suite passing is evidence
	// that the children read nothing here.
	cmd.Dir = t.TempDir()

	client := mcp.NewClient(&mcp.Implementation{Name: "cloudwatch-suite", Version: "v1"}, nil)
	session, err := client.Connect(t.Context(), &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connecting to the spawned collector: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return &stdioProvider{session: session}
}

// advertised returns the child's single advertised tool.
func (p *stdioProvider) advertised(t *testing.T) *mcp.Tool {
	t.Helper()
	list, err := p.session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("listing the collector's tools: %v", err)
	}
	return toolByName(t, list.Tools, toolName)
}

// call invokes the collect tool with raw arguments.
func (p *stdioProvider) call(t *testing.T, args map[string]any) (*mcp.CallToolResult, error) {
	t.Helper()
	return p.session.CallTool(t.Context(), &mcp.CallToolParams{Name: p.advertised(t).Name, Arguments: args})
}

// probeEnv asks the child about a set of names and returns, per name, whether
// it was present and what value it held.
func (p *stdioProvider) probeEnv(t *testing.T, names ...string) map[string]envObservation {
	t.Helper()
	res, err := p.call(t, map[string]any{"id": "probe", "params": map[string]any{"names": names}})
	if err != nil {
		t.Fatalf("the environment probe failed at the transport: %v", err)
	}
	if res.IsError {
		t.Fatalf("the environment probe was refused: %s", refusalText(res, nil))
	}
	nodes, ok := res.StructuredContent.(map[string]any)["nodes"].([]any)
	if !ok {
		t.Fatalf("the probe returned no nodes: %v", res.StructuredContent)
	}
	out := make(map[string]envObservation, len(nodes))
	for _, raw := range nodes {
		node, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("the probe returned a node of an unexpected shape: %v", raw)
		}
		name, _ := node["id"].(string)
		status, _ := node["status"].(string)
		value, _ := node["content"].(string)
		out[name] = envObservation{Present: status == "present", Value: value}
	}
	return out
}

// envObservation is one name's answer from the child.
type envObservation struct {
	Present bool
	Value   string
}

// TestCredentialChainFailureNamesTheChain is the credential arm, driven through
// a REAL spawned child with an EMPTY environment block, which is the state an
// operator who declared no names is in.
//
// It is the loud-failure half of the credential requirement: the collect must
// name the chain rather than return an empty successful graph.
func TestCredentialChainFailureNamesTheChain(t *testing.T) {
	p := dialStdioProviderWithEnv(t, modeServe, nil)
	res, err := p.call(t, map[string]any{
		"id":     "instance-1",
		"params": map[string]any{"log_groups": []any{"/aws/lambda/absent"}},
	})
	if err == nil && (res == nil || !res.IsError) {
		t.Fatalf("a collect with no credential in the child's environment succeeded: %v", res)
	}
	text := refusalText(res, err)
	for _, want := range []string{"credential", "chain"} {
		if !strings.Contains(strings.ToLower(text), want) {
			t.Errorf("the refusal does not name %q: %s", want, text)
		}
	}
	if strings.Contains(text, "nodes") {
		t.Errorf("the refusal looks like a successful empty result: %s", text)
	}
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
