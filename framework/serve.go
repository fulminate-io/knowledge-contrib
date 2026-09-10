// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// serve.go — the TWO ENTRY POINTS, both driving ONE mcp.Server value.
//
// The stdio arm runs that value on the SDK's stdio transport; the HTTP arm
// wraps the same value in a streamable-HTTP handler. Building the server once
// and sharing it is what makes the two transports provably the same provider
// rather than two hand-written approximations that drift.
//
// STDOUT IS THE PROTOCOL STREAM on stdio. A collector that prints to stdout
// corrupts the JSON-RPC framing and surfaces to an operator as an opaque
// handshake failure; every diagnostic a collector writes goes to stderr.

// implementationVersion identifies this framework in the MCP handshake. A
// consumer logs it, so it names the serving layer rather than the package path.
const implementationVersion = "v1"

// httpReadHeaderTimeout bounds how long a client may take to send its request
// headers. The other http.Server timeouts are deliberately left at zero: a
// streamable-HTTP MCP session holds a long-lived response open, so a WriteTimeout
// would cut live sessions at an arbitrary age.
const httpReadHeaderTimeout = 10 * time.Second

// httpShutdownGrace bounds how long [ServeHTTP] waits for in-flight requests
// after its context is cancelled, before it closes the MCP sessions outright.
const httpShutdownGrace = 5 * time.Second

// NewServer builds the MCP server one collector serves: TWO tools — the collect
// tool, carrying the contract's advertised schemas with this collector's params
// spliced in and bound to a handler that validates, walks and encodes; and the
// required describe tool, carrying the checked-in describe schema and bound to a
// handler that answers this collector's declaration.
//
// THE DECLARATION IS VALIDATED HERE, BEFORE ANYTHING SERVES, and that placement
// is the whole of why Describe is a method. A malformed declaration refuses the
// server, so a collector whose vocabulary is empty or whose environment class is
// misspelled fails at its author's first run rather than at an operator's first
// collect — where it would surface as every node being refused by name.
//
// It is exported because a collector with its own transport story (a test, an
// embedding host) needs the value; a collector main calls [ServeStdio] or
// [ServeHTTP] and never sees it.
func NewServer[P any](c Collector[P]) (*mcp.Server, error) {
	tool, name, err := toolFor(c)
	if err != nil {
		return nil, err
	}
	describeTool, decl, err := describeToolFor(c)
	if err != nil {
		return nil, err
	}
	srv := mcp.NewServer(&mcp.Implementation{Name: name, Version: implementationVersion}, nil)
	mcp.AddTool(srv, tool, collectHandler(c, name))
	mcp.AddTool(srv, describeTool, describeHandler(decl))
	return srv, nil
}

// ServeStdio serves this collector over MCP on stdin/stdout and blocks until the
// session ends or ctx is cancelled. It is the entry point for a collector
// installed as a `type: stdio` config entry, which is the shape a daemon spawns.
// It answers `--version` before it builds anything: a collector binary is
// installed by a script that verifies what it placed, and that check needs an
// answer from the binary itself rather than from the archive it came out of. It
// answers the two describe queries the installer's table generator asks on the
// same terms (describe_cli.go). Every such switch is in FIRST POSITION ONLY, so
// an entry's own args reach the collector untouched (version.go states why).
func ServeStdio[P any](ctx context.Context, c Collector[P]) error {
	if serveVersion(c) {
		return nil
	}
	// The two describe queries the installer's table generator asks, answered on
	// the same terms as --version: first position only, before anything serves.
	if serveDescribeQuery(c) {
		return nil
	}
	srv, err := NewServer(c)
	if err != nil {
		return err
	}
	return srv.Run(ctx, &mcp.StdioTransport{})
}

// NewHTTPHandler builds the streamable-HTTP handler serving this collector. The
// closure returns ONE server value for every request, which is what makes the
// HTTP arm the same provider the stdio arm serves.
//
// IT RETURNS THAT SERVER VALUE BESIDE THE HANDLER, and the reason is a leak
// rather than a convenience: an http.Handler owns no session lifecycle, so a
// caller that stops serving still holds live MCP sessions whose reader
// goroutines outlive both the client's close and the listener's. A test drains
// them (frameworktest.DrainServerSessions) and a long-lived host closes them on
// shutdown; a caller that wants neither may ignore the value.
func NewHTTPHandler[P any](c Collector[P]) (http.Handler, *mcp.Server, error) {
	srv, err := NewServer(c)
	if err != nil {
		return nil, nil, err
	}
	return mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return srv },
		&mcp.StreamableHTTPOptions{MaxRequestBodyBytes: unboundedRequestBody},
	), srv, nil
}

// unboundedRequestBody disables the SDK's inbound request-body limit.
//
// COLLECTOR TRAFFIC CARRIES NO SIZE CAP IN EITHER DIRECTION, and this is the
// inbound half of that on the collector's own side. The SDK bounds an incoming
// request body at 4 MiB when the option is left at zero, and answers anything
// larger with 413; a negative value disables it. That default is reachable by
// ordinary use rather than by abuse: a collect whose entry declares cloud or
// code context arrives carrying that declared slice, which crosses 4 MiB on a
// real account long before it is interesting.
//
// THE POSTURE IT ASSUMES IS THE ONE THIS PACKAGE ALREADY DOCUMENTS. ServeHTTP
// adds no authentication and says so; an operator exposing a collector is
// fronting it with something of their own, and the bound removed here was never
// what made that safe. A collector serving untrusted callers wraps this handler
// with its own limit rather than relying on a default it cannot see.
const unboundedRequestBody = -1

// ServeHTTP serves this collector over streamable HTTP on addr and blocks until
// ctx is cancelled or the listener fails. It is the entry point for a collector
// installed as a `type: http` config entry, which is the shape that runs on
// another host.
//
// IT ADDS NO AUTHENTICATION. The transport is served as the contract defines it
// today; an operator fronting it with anything is doing so outside this package.
func ServeHTTP[P any](ctx context.Context, addr string, c Collector[P]) error {
	handler, mcpServer, err := NewHTTPHandler(c)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: httpReadHeaderTimeout,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	go func() {
		<-ctx.Done()
		// A shutdown context of its own: ctx is already done, and passing it
		// would turn every graceful shutdown into an immediate close.
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), httpShutdownGrace)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		// Shutdown drains HTTP requests; it does not end an MCP session, whose
		// reader outlives the request that started it. Closing the sessions is
		// what releases them.
		for ss := range mcpServer.Sessions() {
			_ = ss.Close()
		}
	}()
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("framework: serving %s over streamable HTTP: %w", addr, err)
	}
	return nil
}

// toolFor builds the tool definition one collector serves, and returns the
// resolved tool name beside it so every refusal downstream names the same thing
// an operator sees in their config entry.
func toolFor[P any](c Collector[P]) (*mcp.Tool, string, error) {
	if c == nil {
		return nil, "", fmt.Errorf("framework: no collector was supplied")
	}
	spec := c.Tool()
	name := defaultedToolName(spec.Name)
	input, err := advertisedInputSchema[P]()
	if err != nil {
		return nil, "", err
	}
	return &mcp.Tool{
		Name:         name,
		Description:  spec.Description,
		InputSchema:  input,
		OutputSchema: advertisedOutputSchema(),
	}, name, nil
}

// describeToolFor builds the describe tool definition and returns the validated
// declaration beside it, so the handler closes over a value that has already
// been refused if it was wrong.
//
// IT ADVERTISES THE CONTRACT FILE VERBATIM, on the same rule the collect tool's
// output schema follows and for the same measured reason: an SDK-INFERRED schema
// is REFUSED by the client's comparator, because jsonschema-go infers a Go slice
// as the union ["null","array"] and the comparator reads a scalar type. See
// schema.go.
func describeToolFor[P any](c Collector[P]) (*mcp.Tool, Declaration, error) {
	if c == nil {
		return nil, Declaration{}, fmt.Errorf("framework: no collector was supplied")
	}
	decl := c.Describe()
	if err := decl.validate(); err != nil {
		return nil, Declaration{}, err
	}
	return &mcp.Tool{
		Name: DescribeToolName,
		Description: "Describe this collector: its suggested behavior defaults and field lists, its per-node-type " +
			"overrides, the node and edge types it emits, the environment variables it reads with their class, " +
			"and the foreign-graph context it needs. It takes no arguments.",
		InputSchema:  describeInputSchema(),
		OutputSchema: advertisedDescribeSchema(),
	}, decl, nil
}

// describeInput is the describe tool's In type. It carries NO fields: describe
// asks about the collector, not about a collect, so there is nothing to
// parameterize it by and nothing an operator can get wrong.
type describeInput struct{}

// describeHandler answers the declaration. It performs no work and reads no
// state: the value was built and validated when the server was.
func describeHandler(decl Declaration) mcp.ToolHandlerFor[describeInput, Declaration] {
	return func(_ context.Context, _ *mcp.CallToolRequest, _ describeInput) (*mcp.CallToolResult, Declaration, error) {
		return nil, decl, nil
	}
}

// collectInput is the served tool's In type: the collect id and the collector's
// own params. The advertised input schema is built in schema.go rather than
// inferred from this struct, so its tags decide only how the arguments decode.
//
// THE CONTEXT FIELD IS LOAD-BEARING AND ITS ABSENCE WAS SILENT. The SDK's typed
// handler decodes these arguments with an unmarshaller that has no
// DisallowUnknownFields, so a key this struct does not name is DROPPED rather
// than refused — and the advertised schema carries the context property whether
// this struct names it or not, because that schema is the contract file with
// only params spliced in. Before this field existed a declaring collector was
// sent its block, decoded nothing from it and walked as if the operator had
// declared none.
type collectInput[P any] struct {
	ID      string         `json:"id"`
	Params  P              `json:"params"`
	Context ForeignContext `json:"context"`
}

// collectHandler is the tool's typed handler. Everything before it — decoding
// the arguments and validating them against the advertised input schema — is the
// SDK's generic AddTool, which rejects invalid input BEFORE this runs. That is
// why the raw handler form is not used here: it performs no input validation at
// all, so a collector's own advertised params schema would be decoration.
func collectHandler[P any](c Collector[P], name string) mcp.ToolHandlerFor[collectInput[P], collectOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, in collectInput[P]) (*mcp.CallToolResult, collectOutput, error) {
		// The schema requires the id to be PRESENT and a string; it cannot
		// require it to be non-empty. An empty collect id names no graph
		// instance, so it is refused here rather than walked and discarded
		// downstream.
		if in.ID == "" {
			return nil, collectOutput{}, fmt.Errorf(
				"%s collector: the collect id is empty; it names the graph instance this result lands in", name)
		}
		result, err := c.Walk(ctx, in.ID, in.Params, in.Context)
		if err != nil {
			// A walk failure is a TOOL error, never an empty successful result:
			// the SDK packs a returned error into the call result with IsError
			// set, and the client treats that as a refused collect that writes
			// nothing. An empty complete result here would instead assert a
			// successful walk that found nothing.
			return nil, collectOutput{}, fmt.Errorf("%s collector: the walk failed: %w", name, err)
		}
		out, err := encodeResult(name, result)
		if err != nil {
			return nil, collectOutput{}, err
		}
		return nil, out, nil
	}
}
