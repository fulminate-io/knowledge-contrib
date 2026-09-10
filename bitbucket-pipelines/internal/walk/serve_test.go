// SPDX-License-Identifier: Apache-2.0

package walk_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/fulminate-io/knowledge-contrib/framework"

	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/bbclient"
	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/walk"
)

// serve_test.go — THE COLLECTOR DRIVEN THROUGH THE FRAMEWORK, over a real stdio
// child process.
//
// WHY THIS EXISTS WHEN EVERY OTHER TEST CALLS Walk DIRECTLY. Calling Walk proves
// what the walk does; it proves nothing about what an operator's daemon SEES.
// Between the two sits the framework: it infers this collector's params into a
// JSON schema, advertises that schema inside the contract's input schema, and
// VALIDATES a call's arguments against it before Walk is reached. Nothing else
// in this module asserts that inference, so a params type that inferred into a
// schema refusing every real call would pass every other test here.
//
// THE CHILD IS A REAL PROCESS, re-exec'd from this test binary, because the
// thing under test includes the transport: a stray write to stdout corrupts the
// framing, and only a real child can show that. BOTH SIDES ARE REAL — this
// module's own collector on one end and the SDK's own client on the other — so
// nothing about the seam is stubbed.

// stubModeEnv switches this test binary into the collector it serves. A child
// process sees it set and serves; the parent never does.
const stubModeEnv = "BITBUCKET_PIPELINES_COLLECTOR_SERVE_MODE"

// plantedStdoutEnv makes the child write one line to STDOUT before it serves,
// which is the planted positive for the framing assertion. Without it, a test
// asserting "the child wrote only protocol frames" passes on an assertion that
// could never fire.
const plantedStdoutEnv = "BITBUCKET_PIPELINES_COLLECTOR_PLANT_STDOUT"

// childProviderEnv carries the recorded provider's base URL to the child, so the
// child's collector reads a local server rather than the real one.
const childProviderEnv = "BITBUCKET_PIPELINES_COLLECTOR_FIXTURE_URL"

// serveAsCollector is called from TestMain in the CHILD. It serves this module's
// real collector over the framework's stdio entry point, pointed at the recorded
// provider its parent started, and never returns.
func serveAsCollector() {
	if os.Getenv(plantedStdoutEnv) != "" {
		os.Stdout.WriteString("this line corrupts the protocol framing\n")
	}
	base := os.Getenv(childProviderEnv)
	collector := walk.Collector{Client: func(username, appPassword string) *bbclient.Client {
		return bbclient.NewAt(base, http.DefaultClient, username, appPassword)
	}}
	if err := framework.ServeStdio(context.Background(), collector); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

// childFixture starts the recorded provider the child will read, in the PARENT,
// and returns its URL. It is a separate server from the in-process fixture
// because the child is a different process and cannot share one.
func childFixture(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := recorded[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			fmt.Fprint(w, `{"error":{"message":"Resource not found"}}`)
			return
		}
		fmt.Fprint(w, body)
	}))
	t.Cleanup(server.Close)
	return server.URL
}

// dial re-execs this test binary as the collector and connects to it.
func dial(t *testing.T, extraEnv ...string) *mcp.ClientSession {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("resolving the test binary: %v", err)
	}
	cmd := exec.Command(self)
	cmd.Env = append(os.Environ(),
		stubModeEnv+"=1",
		childProviderEnv+"="+childFixture(t),
		"BITBUCKET_USERNAME=fixture-user",
		"BITBUCKET_APP_PASSWORD=fixture-app-password",
	)
	cmd.Env = append(cmd.Env, extraEnv...)
	// The child's diagnostics go to this process's stderr, so a failure in the
	// child is visible rather than swallowed by the transport.
	cmd.Stderr = os.Stderr

	client := mcp.NewClient(&mcp.Implementation{Name: "bitbucket-pipelines-suite", Version: "v1"}, nil)
	session, err := client.Connect(t.Context(), &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connecting to the collector over stdio: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// TestTheAdvertisedSchemasSatisfyTheContract compares what this collector
// advertises against the framework's own checked-in contract files, rather than
// against a hand-written list of field names — a literal list drifts silently
// when the contract moves.
func TestTheAdvertisedSchemasSatisfyTheContract(t *testing.T) {
	session := dial(t)

	list, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("listing the collector's tools: %v", err)
	}
	// BY NAME, NOT BY COUNT: this collector serves the collect tool and the
	// contract's required describe tool, and this row is about the first.
	tool := toolByName(t, list.Tools, "collect")
	if tool.Description == "" {
		t.Error("the served tool carries no description; an operator installing it reads that")
	}

	for _, row := range []struct {
		what       string
		advertised any
		contract   []byte
	}{
		{"input", tool.InputSchema, framework.InputContractJSON()},
		{"output", tool.OutputSchema, framework.OutputContractJSON()},
	} {
		for _, required := range requiredNames(t, row.contract) {
			if !slices.Contains(requiredNames(t, remarshal(t, row.advertised)), required) {
				t.Errorf("the advertised %s schema does not require %q, which the contract does",
					row.what, required)
			}
		}
	}

	// THE COLLECTOR'S OWN PARAMS reach the caller inside that input schema, which
	// is the half the contract cannot state: without them nothing validates a
	// collect's params.
	advertised := string(remarshal(t, tool.InputSchema))
	for _, want := range []string{`"id"`, `"params"`, `"max_concurrency"`} {
		if !strings.Contains(advertised, want) {
			t.Errorf("the advertised input schema does not carry %s: %s", want, advertised)
		}
	}
	// The control: a name this collector's params do NOT have must be absent, so
	// a schema that mentioned everything would not pass. `workspace` is the one
	// worth naming: it is the parameter this collector deliberately does not take,
	// because the collect id IS the workspace.
	for _, absent := range []string{"workspace", "max_runs", "history_depth"} {
		if strings.Contains(advertised, `"`+absent+`"`) {
			t.Errorf("the advertised schema carries a parameter this collector does not take, "+
				"%q: %s", absent, advertised)
		}
	}
}

// TestACollectOverStdioReturnsTheEnvelope drives the whole path: the SDK
// validates the arguments against the advertised schema, the framework decodes
// them, the walk runs, and the framework encodes the contract envelope.
func TestACollectOverStdioReturnsTheEnvelope(t *testing.T) {
	session := dial(t)

	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "collect",
		Arguments: map[string]any{"id": "acme", "params": map[string]any{"max_concurrency": 3}},
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
	if len(envelope.Nodes) == 0 {
		t.Error("the envelope carries no nodes")
	}
	if len(envelope.Edges) == 0 {
		t.Error("the envelope carries no edges")
	}
	// walk_complete is PRESENT and true. Present matters on its own: the contract
	// requires the key, and an encoding that omitted it on the false arm would
	// fail a collect with a missing-property error.
	if envelope.WalkComplete == nil {
		t.Fatal("the envelope carries no walk_complete key")
	}
	if !*envelope.WalkComplete {
		t.Error("a clean recorded walk reported an incomplete result")
	}
}

// TestAMalformedWorkspaceIsRefusedOverTheTransport closes the loop on the
// validator: the refusal reaches the caller as a tool error rather than as an
// empty successful collect.
func TestAMalformedWorkspaceIsRefusedOverTheTransport(t *testing.T) {
	session := dial(t)

	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "collect",
		Arguments: map[string]any{"id": "acme/api"},
	})
	if err != nil {
		t.Fatalf("calling collect over stdio: %v", err)
	}
	if !res.IsError {
		t.Fatal("a malformed workspace was collected rather than refused")
	}
	if !strings.Contains(resultText(res), "acme/api") {
		t.Errorf("the refusal did not survive the transport intact: %s", resultText(res))
	}
}

// TestAMissingCredentialIsRefusedOverTheTransport. The credential is read from
// the child's environment, so this is the arm an operator hits when the config
// entry's env block does not carry it.
func TestAMissingCredentialIsRefusedOverTheTransport(t *testing.T) {
	session := dial(t, "BITBUCKET_USERNAME=", "BITBUCKET_APP_PASSWORD=")

	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "collect",
		Arguments: map[string]any{"id": "acme"},
	})
	if err != nil {
		t.Fatalf("calling collect over stdio: %v", err)
	}
	if !res.IsError {
		t.Fatal("a collect with no credential returned a successful, empty result")
	}
	for _, want := range []string{"BITBUCKET_USERNAME", "BITBUCKET_APP_PASSWORD"} {
		if !strings.Contains(resultText(res), want) {
			t.Errorf("the refusal does not name %q: %s", want, resultText(res))
		}
	}
}

// TestABoundOfTheWrongKindIsRefusedBeforeTheWalkRuns is the wrong-kind cell of
// the parameter matrix, and the refusal comes from the SCHEMA rather than from
// the walk.
//
// THE TELL IS THE MESSAGE. A value the walk refused names the parameter and this
// collector's own ceiling; a value the schema refused names the type it expected.
// Asserting on the second is what shows the advertised schema is doing the work.
func TestABoundOfTheWrongKindIsRefusedBeforeTheWalkRuns(t *testing.T) {
	session := dial(t)

	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      "collect",
		Arguments: map[string]any{"id": "acme", "params": map[string]any{"max_concurrency": "ten"}},
	})
	if err != nil {
		// The SDK may surface a schema violation as a protocol error rather than
		// as a tool result; either is a refusal, and this is the arm that says so.
		if !strings.Contains(err.Error(), "max_concurrency") &&
			!strings.Contains(err.Error(), "integer") {
			t.Errorf("a non-integer bound was refused with a message naming neither the parameter "+
				"nor the type: %v", err)
		}
		return
	}
	if !res.IsError {
		t.Fatal("a non-integer max_concurrency was accepted")
	}
	text := resultText(res)
	if !strings.Contains(text, "max_concurrency") && !strings.Contains(text, "integer") {
		t.Errorf("the refusal names neither the parameter nor the type: %s", text)
	}
	if strings.Contains(text, "at least 1") || strings.Contains(text, "above the maximum") {
		t.Errorf("the walk's own validator refused it, so the advertised schema is not "+
			"constraining the type: %s", text)
	}
}

// TestAStrayWriteToStdoutBreaksTheHandshake is the framing row, asserted by its
// CONSEQUENCE rather than by inspecting bytes.
//
// stdout is the protocol stream, and the framework writes the frames onto it
// directly — so a test that wanted to read those bytes would have to stand
// between the SDK and the file descriptor, which is a fixture on the side of the
// seam under test. What an operator actually experiences is the handshake
// failing with a message about nothing they can act on, and that is reproducible
// end to end: the SAME child, with one line printed before it serves, cannot be
// dialed.
//
// THE CLEAN ARM IS THE KNOWN POSITIVE, in the same run.
func TestAStrayWriteToStdoutBreaksTheHandshake(t *testing.T) {
	if _, err := dial(t).ListTools(t.Context(), nil); err != nil {
		t.Fatalf("the clean child could not be dialed: %v", err)
	}

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("resolving the test binary: %v", err)
	}
	cmd := exec.Command(self)
	cmd.Env = append(os.Environ(), stubModeEnv+"=1", plantedStdoutEnv+"=1")
	cmd.Stderr = os.Stderr

	client := mcp.NewClient(&mcp.Implementation{Name: "framing-probe", Version: "v1"}, nil)
	session, err := client.Connect(t.Context(), &mcp.CommandTransport{Command: cmd}, nil)
	if err == nil {
		_ = session.Close()
		t.Fatal("a collector that printed a line to stdout before serving was dialed " +
			"successfully. stdout is the protocol stream: if a stray write survives the " +
			"handshake, nothing in this module observes the corruption an operator would hit")
	}
}

// requiredNames is a JSON Schema document's top-level required list.
func requiredNames(t *testing.T, document []byte) []string {
	t.Helper()
	var doc struct {
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(document, &doc); err != nil {
		t.Fatalf("decoding a schema document: %v", err)
	}
	return doc.Required
}

// remarshal re-encodes an advertised schema value as the bytes it went over the
// wire as.
func remarshal(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("re-encoding an advertised schema: %v", err)
	}
	return raw
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
