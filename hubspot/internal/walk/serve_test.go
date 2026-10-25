// SPDX-License-Identifier: Apache-2.0

package walk_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/fulminate-io/knowledge-contrib/framework"

	"github.com/fulminate-io/knowledge-contrib/hubspot/internal/hubapi"
	"github.com/fulminate-io/knowledge-contrib/hubspot/internal/walk"
)

// serveModeEnv switches this test binary into a collector child serving the
// real walk over stdio against the fixture URL in fixtureURLEnv.
const (
	serveModeEnv  = "HUBSPOT_COLLECTOR_WALK_SERVE_MODE"
	fixtureURLEnv = "HUBSPOT_COLLECTOR_WALK_FIXTURE_URL"
)

func TestMain(m *testing.M) {
	if os.Getenv(serveModeEnv) == "1" {
		base := os.Getenv(fixtureURLEnv)
		collector := walk.Collector{Client: func(token string) *hubapi.Client {
			return hubapi.NewAt(base, http.DefaultClient, token)
		}}
		if err := framework.ServeStdio(context.Background(), collector); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// TestACollectOverStdioReturnsACompleteEnvelope drives the real collector in a
// child process through the SDK's client: the arguments, the search's filter
// groups included, are validated against the advertised schema, the walk reads
// the fixture, and the envelope carries the corpus with walk_complete present
// and true.
func TestACollectOverStdioReturnsACompleteEnvelope(t *testing.T) {
	f := newFixture(t, corpus())

	self, err := os.Executable()
	if err != nil {
		t.Fatalf("resolving the test binary: %v", err)
	}
	cmd := exec.Command(self)
	cmd.Env = []string{
		serveModeEnv + "=1",
		fixtureURLEnv + "=" + f.server.URL,
		walk.TokenVariable + "=" + fixtureToken,
		"PATH=" + os.Getenv("PATH"),
	}
	cmd.Stderr = os.Stderr
	client := mcp.NewClient(&mcp.Implementation{Name: "hubspot-walk-suite", Version: "v1"}, nil)
	session, err := client.Connect(t.Context(), &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("connecting to the collector child: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: "collect",
		Arguments: map[string]any{"id": "acme", "params": map[string]any{
			"companies": []any{"101"},
			"contacts":  []any{"201"},
			"deals":     []any{"301"},
			"lists":     []any{"7"},
			"searches": []any{map[string]any{"object_type": "contacts", "filter_groups": []any{map[string]any{"filters": []any{
				map[string]any{"propertyName": "lifecyclestage", "operator": "EQ", "value": "customer"}}}}}},
		}},
	})
	if err != nil {
		t.Fatalf("calling collect: %v", err)
	}
	if res.IsError {
		raw, err := json.Marshal(res.Content)
		if err != nil {
			t.Fatalf("the collect was refused, and its content did not encode: %v", err)
		}
		t.Fatalf("the collect was refused: %s", raw)
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Nodes []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"nodes"`
		WalkComplete *bool `json:"walk_complete"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("decoding the envelope: %v", err)
	}
	if envelope.WalkComplete == nil || !*envelope.WalkComplete {
		t.Fatalf("walk_complete is absent or false: %s", raw)
	}
	if len(envelope.Nodes) != 16 {
		t.Errorf("the envelope carries %d nodes, want the corpus's 16: 2 companies, 6 contacts, a deal, 6 engagement and ticket records and a list", len(envelope.Nodes))
	}
	if strings.Contains(string(raw), fixtureToken) {
		t.Errorf("the envelope carries the token")
	}
}
