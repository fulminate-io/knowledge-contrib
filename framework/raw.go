// SPDX-License-Identifier: Apache-2.0

package framework

import "github.com/modelcontextprotocol/go-sdk/mcp"

// raw.go — THE FRAMEWORK-INTERNAL ESCAPE, and the two negatives it exists to
// make observable.
//
// The ordinary path installs the SDK's GENERIC AddTool, which validates a call's
// arguments against the advertised input schema before the handler runs and
// validates the handler's Out value against the advertised output schema before
// the result leaves the provider. Both of those are requirements, and a
// requirement nothing can violate is a requirement nothing tests.
//
// So this file provides the same tool definition bound to the SDK's RAW
// ToolHandler, which does neither. It is UNEXPORTED and is used by this
// module's own negative tests only: the "the generic form is what refuses bad
// params" arm needs a provider that runs the walk on params its own schema
// refuses, and the "provider breaks its own word" arm needs one that returns a
// payload its own advertised output schema forbids. Exporting it would hand a
// collector author a way to serve a tool that validates nothing while still
// advertising that it does.
//
// This is the same reason, in the same words, that the client's own stub
// provider gives for using the raw form in its tests.

// newRawServer builds a server advertising exactly the schemas [NewServer]
// advertises for this collector, with a caller-supplied RAW handler in place of
// the validating one.
func newRawServer[P any](c Collector[P], h mcp.ToolHandler) (*mcp.Server, error) {
	tool, name, err := toolFor(c)
	if err != nil {
		return nil, err
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: name, Version: implementationVersion}, nil)
	srv.AddTool(tool, h)
	return srv, nil
}
