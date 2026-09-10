// SPDX-License-Identifier: Apache-2.0

package walk_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// serve_harness_test.go — reading what came back over the transport.
//
// EVERY HELPER HERE DECODES SOMETHING THE SDK HANDED BACK in a shape that
// depends on how the caller asked for it: an advertised schema arrives as an
// any, and a tool result carries its payload as structured content or as text
// depending on whether the caller declared an output type. Keeping the decoding
// beside the assertions would make each row look like it were asserting about
// the wire when it is asserting about the collector.

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
