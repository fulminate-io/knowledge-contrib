// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"github.com/fulminate-io/knowledge-contrib/framework"
	"github.com/fulminate-io/knowledge-contrib/framework/frameworktest"
	"github.com/fulminate-io/knowledge-contrib/k8s-logs/internal/k8slogs"
	"github.com/fulminate-io/knowledge-contrib/k8s-logs/internal/logpipe"
)

// stdio_test.go — THE STDIO ROUND TRIP, over a real child process.
//
// THE PROVIDER IS THIS TEST BINARY, RE-EXECED. TestMain reads one switch
// variable and, when it is set, serves MCP on stdin and stdout instead of
// running tests — so the child is a real process speaking the real protocol,
// with no compile step, no module scaffolding in a temporary directory and no
// network. The alternative, driving the server value in-process, would not
// exercise the thing this file is about: the FRAMING on a real pipe.
//
// WHAT IT PROVES HERE: the handshake completes, the tool is listed under the
// name the config entry's `tool` field must carry, a call returns a payload
// carrying only envelope fields, and the child's stdout carried protocol and
// nothing else. A stray print from this module's own code would corrupt the
// framing and surface as an opaque handshake failure rather than as an error,
// which is exactly the failure this round trip catches.
//
// NOTHING HERE CONTACTS ANY DAEMON, SERVER OR CLUSTER. The child's Kubernetes
// endpoint is an httptest server this test starts, and its environment is built
// from scratch.

// The switch variables the re-exec is driven by.
const (
	serveSwitchEnv = "K8S_LOGS_TEST_SERVE"
	apiEndpointEnv = "K8S_LOGS_TEST_API"
)

func TestMain(m *testing.M) {
	if os.Getenv(serveSwitchEnv) == "" {
		// The goroutine-leak gate wraps the whole package's run; see
		// leakguard_test.go for what it observes and why it is installed here.
		frameworktest.VerifyNoGoroutineLeaks(m)
		return
	}
	// The child arm. Diagnostics go to stderr; stdout is the protocol stream.
	//
	// THE ENDPOINT IS REQUIRED, and the refusal is what makes a claim about this
	// child provable rather than incidental. Without it the collector would fall
	// back to the standard kubeconfig resolution and READ FILES — a kubeconfig,
	// a token, whatever the auth plugin reaches for — and a child's reads are
	// invisible to the parent's test-cache key. Refusing here means the child's
	// only input is an HTTP endpoint the parent started, under every invocation
	// rather than under the ones that happened to set the variable.
	endpoint := os.Getenv(apiEndpointEnv)
	if endpoint == "" {
		fmt.Fprintf(os.Stderr, "k8s-logs test provider: %s is unset; this child serves only against a "+
			"test endpoint, never against a real kubeconfig\n", apiEndpointEnv)
		os.Exit(2)
	}
	collector := &k8slogs.Collector{NewClient: func(string) (kubernetes.Interface, error) {
		return kubernetes.NewForConfig(&rest.Config{Host: endpoint})
	}}
	if err := framework.ServeStdio(context.Background(), collector); err != nil {
		fmt.Fprintf(os.Stderr, "k8s-logs test provider: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

// dialProvider spawns this test binary as an MCP stdio provider and returns a
// connected session.
func dialProvider(t *testing.T, apiEndpoint string) *mcp.ClientSession {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(self)
	// BUILT FROM SCRATCH, never inherited: under the collector contract the
	// entry's env block is the child's whole environment, so a test that let
	// the parent's environment through would be testing something no deployed
	// collector experiences.
	cmd.Env = []string{
		serveSwitchEnv + "=1",
		apiEndpointEnv + "=" + apiEndpoint,
	}
	cmd.Stderr = os.Stderr
	// THE CHILD RUNS OUTSIDE THE WORKSPACE, and that is a measurement rather
	// than tidiness. A child's file opens are invisible to the PARENT's
	// test-cache key, so a child that read anything under this module would make
	// every test in this package cacheable against a subject it never read.
	// Running it from a directory with nothing in it means the whole suite
	// passing IS the proof that no workspace-relative read is load-bearing:
	// if one were, it would fail here.
	cmd.Dir = t.TempDir()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	client := mcp.NewClient(&mcp.Implementation{Name: "k8s-logs-test", Version: "v1"}, nil)
	session, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("the MCP handshake with the spawned provider failed: %v. A write to stdout that is not a "+
			"protocol message corrupts the framing and surfaces exactly like this", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// TestStdioRoundTripListsTheToolAndReturnsAValidatedResult.
func TestStdioRoundTripListsTheToolAndReturnsAValidatedResult(t *testing.T) {
	api := newStubAPI(t)
	session := dialProvider(t, api.URL)

	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	tool := toolByName(t, tools.Tools, k8slogs.ToolName)
	if tool.Name != k8slogs.ToolName {
		t.Fatalf("the served tool is %q; the config entry's `tool` field must name it, and it names %q",
			tool.Name, k8slogs.ToolName)
	}
	if tool.InputSchema == nil || tool.OutputSchema == nil {
		t.Fatal("the tool advertises no schema; the client's gate checks both at registration and at collect")
	}

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: k8slogs.ToolName,
		Arguments: map[string]any{
			"id": "dev-window",
			"params": map[string]any{
				"namespaces":           []string{"dev"},
				"chunk_window_seconds": 3600,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("the collect returned a tool error: %v", res.Content)
	}

	payload := decodePayload(t, res)
	if len(payload.Nodes) == 0 {
		t.Fatal("the collect returned no nodes from a stub API serving one pod with a log")
	}
	if !payload.WalkComplete {
		t.Fatal("the collect asserted an incomplete walk over a clean stub read")
	}

	types := map[string]int{}
	for _, n := range payload.Nodes {
		types[n.Type]++
	}
	for _, want := range []string{
		logpipe.NodeLogTemplate, logpipe.NodeLogStream, logpipe.NodeLogChunk, logpipe.NodeLogLabel,
	} {
		if types[want] == 0 {
			t.Errorf("the round trip returned no %q node; node types present: %v", want, types)
		}
	}
}

// TestTheResultCarriesOnlyEnvelopeFields — a field the envelope does not name
// is an error on the way in, not a silent drop, so it is caught here by
// decoding the payload with unknown fields refused.
func TestTheResultCarriesOnlyEnvelopeFields(t *testing.T) {
	api := newStubAPI(t)
	session := dialProvider(t, api.URL)

	res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name:      k8slogs.ToolName,
		Arguments: map[string]any{"id": "dev-window", "params": map[string]any{"namespaces": []string{"dev"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	var strict struct {
		Nodes        []framework.Node `json:"nodes"`
		Edges        []framework.Edge `json:"edges"`
		WalkComplete bool             `json:"walk_complete"`
	}
	if err := dec.Decode(&strict); err != nil {
		t.Fatalf("the result carries a field the contract envelope does not name: %v\n%s", err, raw)
	}
}

// TestAnInvalidCollectIsAToolErrorNotAnEmptySuccess — the walk's refusals reach
// the caller as errors, and never as a successful collect that found nothing.
// An empty complete result would assert that the source really is empty, and
// the server treats a complete collect's absent rows as deleted.
func TestAnInvalidCollectIsAToolErrorNotAnEmptySuccess(t *testing.T) {
	api := newStubAPI(t)
	session := dialProvider(t, api.URL)

	for _, tc := range []struct {
		name string
		args map[string]any
	}{
		{"no namespaces", map[string]any{"id": "x", "params": map[string]any{}}},
		{"an inverted range", map[string]any{"id": "x", "params": map[string]any{
			"namespaces": []string{"dev"},
			"since":      "2026-09-07T11:00:00Z",
			"until":      "2026-09-07T10:00:00Z",
		}}},
		{"a zero bound", map[string]any{"id": "x", "params": map[string]any{
			"namespaces": []string{"dev"}, "tail_lines": 0,
		}}},
		{"an empty collect id", map[string]any{"id": "", "params": map[string]any{
			"namespaces": []string{"dev"},
		}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
				Name: k8slogs.ToolName, Arguments: tc.args,
			})
			if err != nil {
				return // a transport-level refusal is also a refusal
			}
			if !res.IsError {
				t.Fatalf("a refused collect came back as a SUCCESS: %v", res.StructuredContent)
			}
		})
	}
}

// decodePayload reads the contract envelope out of a tool result.
func decodePayload(t *testing.T, res *mcp.CallToolResult) struct {
	Nodes        []framework.Node `json:"nodes"`
	Edges        []framework.Edge `json:"edges"`
	WalkComplete bool             `json:"walk_complete"`
} {
	t.Helper()
	var out struct {
		Nodes        []framework.Node `json:"nodes"`
		Edges        []framework.Edge `json:"edges"`
		WalkComplete bool             `json:"walk_complete"`
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("the tool result does not decode as the contract envelope: %v\n%s", err, raw)
	}
	return out
}

// newStubAPI is the child's Kubernetes endpoint: one namespace, one pod, one
// container with two stamped lines.
func newStubAPI(t *testing.T) *httptest.Server {
	t.Helper()
	base := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/namespaces/data/pods") && strings.HasSuffix(r.URL.Path, "/pods"):
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"apiVersion":"v1","kind":"PodList","items":[{"apiVersion":"v1","kind":"Pod",`+
				`"metadata":{"name":"db-1","namespace":"data"},`+
				`"spec":{"containers":[{"name":"db"}]}}]}`)
		case strings.HasSuffix(r.URL.Path, "/pods"):
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"apiVersion":"v1","kind":"PodList","items":[{"apiVersion":"v1","kind":"Pod",`+
				`"metadata":{"name":"api-1","namespace":"dev"},`+
				`"spec":{"containers":[{"name":"api"}]}}]}`)
		case strings.Contains(r.URL.Path, "/db-1/log"):
			// The SECOND service's error, at the same instant as the first, so
			// the two templates overlap in time across two services — which is
			// what a candidate correlation is.
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprintf(w, "%s [ERROR] refused a connection past the pool limit\n",
				base.Format(time.RFC3339Nano))
		case strings.HasSuffix(r.URL.Path, "/log"):
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprintf(w, "%s [ERROR] connection to database failed for alpha\n%s [ERROR] connection to database failed for beta\n",
				base.Format(time.RFC3339Nano), base.Add(time.Second).Format(time.RFC3339Nano))
		default:
			http.Error(w, "unexpected path "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}
