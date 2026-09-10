// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// tools_by_name_test.go — SELECTING A SERVED TOOL BY NAME.
//
// Every collector serves TWO tools: the collect tool a config entry names, and
// the contract's required describe tool. A row that took the listing's single
// entry, or asserted a cardinality, would have to be re-tuned every time the
// contract gains a tool — and a re-tuned count is a row that passes for the
// wrong reason. Selecting by name says what the row is about, and a name that is
// not served fails naming what WAS.
func toolByName(t *testing.T, tools []*mcp.Tool, name string) *mcp.Tool {
	t.Helper()
	served := make([]string, 0, len(tools))
	for _, tool := range tools {
		if tool != nil && tool.Name == name {
			return tool
		}
		if tool != nil {
			served = append(served, tool.Name)
		}
	}
	t.Fatalf("the collector does not serve a tool named %q; it serves: %s", name, strings.Join(served, ", "))
	return nil
}
