// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/fulminate-io/knowledge-contrib/framework/frameworktest"
)

// serve_test.go — the TWO-TRANSPORT HARNESS and the matrix. The envelope's own
// rows are in envelope_test.go and the two source censuses are in
// census_test.go; all three drive the harness below.

// The transport axis every table runs twice over.
const (
	overStdio = "stdio"
	overHTTP  = "streamable-http"
)

// The fixture really does satisfy the interface the framework serves, with its
// params type spelled out, checked at compile time.
var _ Collector[fixtureParams] = (*fixtureCollector)(nil)

// provider is one dialed fixture collector: the client session, and — on the
// in-process arm only — the fixture value itself, so a test can read whether the
// walk ran rather than infer it from a refusal.
type provider struct {
	transport string
	session   *mcp.ClientSession
	fixture   *fixtureCollector // nil on the stdio arm: the walk runs in a child
	// toolName is the collect tool this provider was asked to serve, carried on
	// BOTH arms: the stdio arm holds no fixture value, and a helper that read the
	// name off the fixture would resolve the default there and look up a tool the
	// child does not serve.
	toolName string
}

// dial starts the fixture collector on one transport and returns a connected
// client session, registering every teardown the leak gate needs.
func dial(t *testing.T, transport string, mode fixtureMode, tool string) *provider {
	t.Helper()
	switch transport {
	case overStdio:
		return dialStdio(t, mode, tool)
	case overHTTP:
		return dialHTTP(t, mode, tool)
	default:
		t.Fatalf("unknown transport %q", transport)
		return nil
	}
}

// dialStdio spawns this test binary as a real stdio collector and speaks
// JSON-RPC to it over its own pipes. Both sides are real: the child runs the
// framework's shipped ServeStdio entry point, the parent is the SDK's client.
func dialStdio(t *testing.T, mode fixtureMode, tool string) *provider {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("resolving the test binary: %v", err)
	}
	cmd := exec.Command(self)
	cmd.Env = append(os.Environ(),
		fixtureModeEnv+"="+string(mode),
		fixtureToolEnv+"="+tool,
	)
	cmd.Stderr = os.Stderr

	client := mcp.NewClient(&mcp.Implementation{Name: "framework-suite", Version: "v1"}, nil)
	session, err := client.Connect(t.Context(), &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connecting to the stdio collector: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return &provider{transport: overStdio, session: session, toolName: tool}
}

// dialHTTP serves the fixture in this process behind an httptest server.
func dialHTTP(t *testing.T, mode fixtureMode, tool string) *provider {
	t.Helper()
	fixture := &fixtureCollector{mode: mode, name: tool}
	handler, mcpServer, err := NewHTTPHandler[fixtureParams](fixture)
	if err != nil {
		t.Fatalf("building the HTTP handler: %v", err)
	}
	httpServer := httptest.NewServer(handler)

	client := mcp.NewClient(&mcp.Implementation{Name: "framework-suite", Version: "v1"}, nil)
	session, err := client.Connect(t.Context(), &mcp.StreamableClientTransport{Endpoint: httpServer.URL}, nil)
	if err != nil {
		t.Fatalf("connecting to the HTTP collector: %v", err)
	}
	t.Cleanup(func() {
		_ = session.Close()
		httpServer.Close()
		// THE DRAIN, and it is not redundant with the two closes above: with
		// both of them run and an empty goleak allowlist, the SDK's
		// streamableServerConn.Read goroutine is still alive at the end of the
		// binary. Closing the server's sessions is what ends it.
		frameworktest.DrainServerSessions(mcpServer)
	})
	return &provider{transport: overHTTP, session: session, fixture: fixture, toolName: tool}
}

// advertised returns the named tool from the listing.
//
// IT SELECTS BY NAME AND ASSERTS NOTHING ABOUT CARDINALITY, exactly as the
// client's own findTool does. It used to require the listing to hold exactly one
// tool, which was true when a collector served one; a collector now serves the
// collect tool AND the required describe tool, and a count assertion here would
// have to be re-tuned every time the contract gains a tool — which is the shape
// that makes a third tool a suite-wide red rather than a fact. A name that is
// not served fails naming what WAS served, so a typo is still loud.
func (p *provider) advertised(t *testing.T, name string) *mcp.Tool {
	t.Helper()
	list, err := p.session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("[%s] listing tools: %v", p.transport, err)
	}
	served := make([]string, 0, len(list.Tools))
	for _, tool := range list.Tools {
		if tool.Name == name {
			return tool
		}
		served = append(served, tool.Name)
	}
	t.Fatalf("[%s] the collector does not serve a tool named %q; it serves: %s",
		p.transport, name, strings.Join(served, ", "))
	return nil
}

// collectTool returns the tool this fixture serves the collect on, whatever it
// was named.
func (p *provider) collectTool(t *testing.T) *mcp.Tool {
	t.Helper()
	return p.advertised(t, p.collectToolName())
}

// collectToolName is the name the fixture's ToolSpec resolves to.
func (p *provider) collectToolName() string {
	if p.toolName != "" {
		return p.toolName
	}
	return DefaultToolName
}

// call invokes the collect tool with raw arguments.
func (p *provider) call(t *testing.T, args map[string]any) (*mcp.CallToolResult, error) {
	t.Helper()
	return p.session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      p.collectTool(t).Name,
		Arguments: args,
	})
}

// envelopeOf decodes a successful call's structuredContent.
func envelopeOf(t *testing.T, res *mcp.CallToolResult) map[string]any {
	t.Helper()
	if res.StructuredContent == nil {
		t.Fatalf("the result carries no structuredContent: %s", resultText(res))
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("re-marshaling structuredContent: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("decoding structuredContent: %v", err)
	}
	return doc
}

// resultText renders a call result's text content, which is where the SDK packs
// a tool error's message.
func resultText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

// assertArray is what distinguishes an EMPTY ARRAY from a JSON null. A nil Go
// slice marshals to null, the contract declares both lists as arrays, and that
// difference is the whole of the empty-walk row.
func assertArray(t *testing.T, doc map[string]any, key string, want int) {
	t.Helper()
	raw, present := doc[key]
	if !present {
		t.Fatalf("the envelope carries no %q at all: %s", key, mustJSON(t, doc))
	}
	list, ok := raw.([]any)
	if !ok {
		t.Fatalf("%q is %T (%v), not a JSON array; a nil Go slice reaching the wire as null is what this asserts against",
			key, raw, raw)
	}
	if len(list) != want {
		t.Errorf("%q carries %d entries, want %d", key, len(list), want)
	}
}

// mustJSON renders a value as compact JSON for comparison and for a failure
// message a reader can diff.
func mustJSON(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshaling %T: %v", v, err)
	}
	return string(raw)
}

// -------------------------------------------------------------------- rows --

// TestBothTransportsServeOneProvider is row 5: one server value, two
// transports, and the assertion that neither the advertised schemas nor the
// returned envelope differ between them. A drift here is a collector whose
// behavior depends on how an operator installed it.
func TestBothTransportsServeOneProvider(t *testing.T) {
	stdioArm := dial(t, overStdio, fixtureConforming, "")
	httpArm := dial(t, overHTTP, fixtureConforming, "")

	// The COLLECT tool on each arm, taken by name: a listing now holds two tools
	// and comparing "whichever sorts first" would compare describe to describe
	// and say nothing about the tool this row is for.
	stdioTool, httpTool := stdioArm.collectTool(t), httpArm.collectTool(t)
	if stdioTool.Name != DefaultToolName || httpTool.Name != DefaultToolName {
		t.Fatalf("tool names differ or are not the default: stdio=%q http=%q", stdioTool.Name, httpTool.Name)
	}
	if got, want := mustJSON(t, httpTool.InputSchema), mustJSON(t, stdioTool.InputSchema); got != want {
		t.Errorf("the two transports advertise different INPUT schemas:\n stdio: %s\n  http: %s", want, got)
	}
	if got, want := mustJSON(t, httpTool.OutputSchema), mustJSON(t, stdioTool.OutputSchema); got != want {
		t.Errorf("the two transports advertise different OUTPUT schemas:\n stdio: %s\n  http: %s", want, got)
	}

	args := map[string]any{"id": "inst-7", "params": map[string]any{"region": "us-east-1"}}
	stdioRes, err := stdioArm.call(t, args)
	if err != nil {
		t.Fatalf("stdio collect: %v", err)
	}
	httpRes, err := httpArm.call(t, args)
	if err != nil {
		t.Fatalf("http collect: %v", err)
	}
	if got, want := mustJSON(t, envelopeOf(t, httpRes)), mustJSON(t, envelopeOf(t, stdioRes)); got != want {
		t.Errorf("the two transports returned different envelopes:\n stdio: %s\n  http: %s", want, got)
	}
}

// TestServedToolNameIsTheCollectors is the tool-name clause: the name comes from
// the collector's own ToolSpec, because a config entry names the tool it calls
// and an operator legitimately serves collect_logs beside another's collect.
func TestServedToolNameIsTheCollectors(t *testing.T) {
	for _, transport := range []string{overStdio, overHTTP} {
		t.Run(transport, func(t *testing.T) {
			p := dial(t, transport, fixtureConforming, "collect_logs")
			if got := p.collectTool(t).Name; got != "collect_logs" {
				t.Errorf("served tool name is %q, want collect_logs", got)
			}
		})
	}
}

// TestCollectMatrix is row 22: two transports times FIVE param classes times
// four result shapes (two completeness values times two result sizes), with the
// behavior each cell holds. A feature that reaches one arm and not its sibling
// is a failing cell here rather than a discovery in production.
//
// The param axis is five rather than the list's three; see the table below for
// which of the two added classes each of Go's decode and the advertised schema
// can refuse, and why only the schema can refuse those two.
func TestCollectMatrix(t *testing.T) {
	shapes := []struct {
		name         string
		mode         fixtureMode
		wantComplete bool
		wantNodes    int
		wantEdges    int
	}{
		{"nonempty-complete", fixtureConforming, true, 2, 1},
		{"nonempty-incomplete", fixtureIncompleteWalk, false, 2, 1},
		{"empty-complete", fixtureEmptyWalk, true, 0, 0},
		{"empty-incomplete", fixtureEmptyIncomplete, false, 0, 0},
	}
	// THE PARAM AXIS HAS FIVE CLASSES, AND THREE OF THEM ARE REFUSALS THAT
	// DISCRIMINATE DIFFERENT THINGS. wrong-type is refused by Go's own unmarshal
	// into the params struct as well as by the schema, so on its own it does NOT
	// show that the collector's schema is what validates: splicing a permissive
	// {"type":"object"} in place of the inferred params sub-schema leaves that
	// cell green, because 7 still fails to unmarshal into a string. The last two
	// classes are the ones Go's decode would let through — it zero-fills a
	// missing field and silently ignores an unknown key — so they are refused
	// ONLY because the framework advertises the collector's own schema and the
	// SDK enforces it before the handler.
	//
	// wantIn is the substring the refusal must carry; empty means the call is
	// expected to succeed.
	params := []struct {
		name   string
		args   map[string]any
		wantIn string
	}{
		{"absent", map[string]any{"id": "inst-7"}, ""},
		{"valid", map[string]any{"id": "inst-7", "params": map[string]any{"region": "us-east-1", "depth": 3}}, ""},
		{"wrong-type", map[string]any{"id": "inst-7", "params": map[string]any{"region": 7}}, "region"},
		// Present, decodes cleanly into fixtureParams, and omits the key the
		// collector's params schema marks required.
		{"incomplete", map[string]any{"id": "inst-7", "params": map[string]any{"depth": 3}},
			`required: missing properties: ["region"]`},
		// Present, valid on every declared key, and carrying one the params type
		// does not declare. Go's decode drops it without a word; the inferred
		// schema sets additionalProperties false and the SDK refuses it.
		{"unknown-key", map[string]any{"id": "inst-7", "params": map[string]any{"region": "us-east-1", "surprise": "x"}},
			`unexpected additional properties ["surprise"]`},
	}

	for _, transport := range []string{overStdio, overHTTP} {
		for _, shape := range shapes {
			for _, param := range params {
				t.Run(fmt.Sprintf("%s/%s/%s", transport, shape.name, param.name), func(t *testing.T) {
					p := dial(t, transport, shape.mode, "")
					res, err := p.call(t, param.args)
					if err != nil {
						t.Fatalf("the call failed at the protocol level: %v", err)
					}

					if param.wantIn != "" {
						assertParamRefusal(t, p, res, param.wantIn)
						return
					}
					if res.IsError {
						t.Fatalf("the collect was refused: %s", resultText(res))
					}

					doc := envelopeOf(t, res)
					assertArray(t, doc, "nodes", shape.wantNodes)
					assertArray(t, doc, "edges", shape.wantEdges)
					complete, ok := doc["walk_complete"].(bool)
					if !ok {
						t.Fatalf("walk_complete is absent or not a boolean in %s", mustJSON(t, doc))
					}
					if complete != shape.wantComplete {
						t.Errorf("walk_complete is %v, want %v", complete, shape.wantComplete)
					}
					assertWalkReceived(t, p, param.name)
				})
			}
		}
	}
}

// assertWalkReceived checks what the walk was handed, where this process can see
// it: absent params reach the walk as the zero value, valid ones verbatim.
func assertWalkReceived(t *testing.T, p *provider, paramClass string) {
	t.Helper()
	if p.fixture == nil {
		return
	}
	call, ran := p.fixture.lastCall()
	if !ran {
		t.Fatalf("the walk never ran on an accepted collect")
	}
	if call.id != "inst-7" {
		t.Errorf("the walk received id %q, want inst-7", call.id)
	}
	want := fixtureParams{}
	if paramClass == "valid" {
		want = fixtureParams{Region: "us-east-1", Depth: 3}
	}
	if call.params != want {
		t.Errorf("the walk received params %+v, want %+v", call.params, want)
	}
}

// assertParamRefusal is a refused param cell's whole behavior: a tool error
// carrying the refusal this class earns, no envelope, and a walk that never ran.
//
// wantIn is per class rather than a single field name, and that is the point of
// the axis: "region" alone is satisfied by Go's unmarshal, while the required-key
// and additional-property refusals can only come from the schema the framework
// advertised on the collector's behalf.
func assertParamRefusal(t *testing.T, p *provider, res *mcp.CallToolResult, wantIn string) {
	t.Helper()
	if !res.IsError {
		t.Fatalf("params the advertised schema refuses were ACCEPTED: %s", mustJSON(t, envelopeOf(t, res)))
	}
	text := resultText(res)
	if !strings.Contains(text, wantIn) {
		t.Errorf("the refusal does not carry %q: %s", wantIn, text)
	}
	if res.StructuredContent != nil {
		t.Errorf("a refused collect carries an envelope: %v", res.StructuredContent)
	}
	if p.fixture != nil && p.fixture.callCount() != 0 {
		t.Errorf("the walk ran %d times on params the advertised schema refuses; it must not run at all",
			p.fixture.callCount())
	}
}
