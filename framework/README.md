# The knowledge collector framework

A knowledge collector is an ordinary process that speaks MCP and answers with
nodes and edges. The daemon spawns it, calls one tool, and writes what comes
back into a graph of its own. Nothing about it is privileged: the collectors
published in this repository and a collector you write on a laptop are
registered the same way and dialed by the same code.

This module is the Go framework those collectors are built on. It serves the
MCP tools, advertises the contract schemas, validates the call, and encodes the
result, so a collector author writes a walk and a params type and nothing else.

The contract itself is language-neutral. Everything under "The wire contract"
below is what a collector must do in any language; everything under "What this
module adds" is convenience this module supplies for Go, and a port in another
language reimplements it or does without it.

## The wire contract

A collector is an MCP server speaking **revision 2025-06-18 or later**. That is
a hard floor rather than a preference: `outputSchema` and `structuredContent`
exist only from that revision, and the contract is built on both, so a provider
that speaks only 2024-11-05 cannot satisfy it.

Over stdio the handshake is the transport's whole setup, and this is what a
collector built on this module answers a current client:

```jsonc
// initialize result
{
  "protocolVersion": "2026-07-28",
  "serverInfo": { "name": "collect", "version": "v1" }
}
```

The revision in that result is **negotiated, not constant**: the client asks for
the newest revision it speaks and the server answers with the newest it can
serve, so the value moves as either side's SDK moves. What does not move is the
floor. Do not hard-code the negotiated value, and do not read a collector's
answer as a promise about the next one.

`serverInfo.name` is the **served tool's name**, not the collector's or the
binary's: a collector serving the default tool identifies as `collect`, and one
serving `collect_aws` identifies as `collect_aws`. `serverInfo.version` is this
serving layer's own version, `v1`, and it is not the binary's build stamp; the
stamp is reachable only by running the binary with `--version`.

**Stdout is the protocol stream.** A collector that prints to stdout corrupts
the JSON-RPC framing, and it reaches an operator as an opaque handshake failure
rather than as the message that was printed. Diagnostics go to stderr.

### Three checked-in JSON Schema files are the contract

The contract is three documents, 21, 46 and 88 lines, all in this module's
`contract/` directory and byte-identical to the copies the client checks in.
They are checked in, rather than generated from a Go type, because the reader
they exist for is writing a collector in another language.

Between them they require **eight properties**:

| Direction | Property | Required |
|---|---|---|
| in | `id` | the collect id, a string |
| out | `nodes` | an array; every item requires `id` and `type` |
| out | `edges` | an array; every item requires `from_id`, `to_id` and `type` |
| out | `walk_complete` | a boolean, the completeness assertion |
| describe | `behavior` | the suggested `summarize`, `embed` and `sync` defaults with the three field lists |
| describe | `node_types` | the node type vocabulary this collector emits |
| describe | `edge_types` | the edge type vocabulary this collector emits |
| describe | `environment` | the environment variable names, each with its class |

Neither document uses a single `enum` keyword. Nothing in the contract
constrains a node type, an edge type or a metadata key to a fixed set: what a
collector may emit is decided by what it declares, not by the schema.

Your tool's advertised `inputSchema` must declare at least the required part of
the input document, which is the collect `id` alone. It may be stricter inside
`params` and never looser. Your `outputSchema` must declare at least the output
document. Both are checked twice: by `knowledge collector add`, which is why
that command dials your provider before it writes anything, and again on every
collect before the call.

### The optional vocabulary is 16 node fields and 9 edge fields

Beyond the required pair, a node carries an optional vocabulary of 16 fields
and an edge one of 9. Both are listed in the guide and mirrored by this module's
`Node` and `Edge` types. Anything outside the vocabulary rides in a node's
`metadata` map. The server's own bookkeeping fields — the created, updated and
tombstoned stamps and the collect epoch — are deliberately absent from both, so
a collector cannot set them.

### Three wire-shaping rules a port has to reproduce

These are measured properties of the client's gate rather than style, and a port
that skips one is refused by a client it never sees:

1. **The output schema is advertised verbatim from the contract file.** A schema
   inferred from a Go slice — or from an equivalent optional array in another
   language — is emitted as the union type `["null","array"]`, and the client's
   comparator reads a scalar type, finds none, and reports that the tool
   declares no type at all.
2. **The input schema is the contract file with only `params` replaced.** Infer
   the whole input from your own request type and `params` lands on the
   top-level required list, which refuses every collect that carries no params
   before the call is even sent. Splice your params sub-schema into the contract
   document and the top-level `required` list stays `["id"]` by construction.
3. **The envelope carries all three output properties, always, and never a
   null array.** `walk_complete` omitted when false fails the call with a
   missing-property error, and it fails only on the incomplete arm, so the
   defect is invisible on every complete walk. A walk that found nothing emits
   `[]` for `nodes` and `edges`, never `null`.

### An incomplete walk says so, and that is what protects the graph

`walk_complete` is required so that silence cannot disable it. A collect
asserting a complete walk lets the server treat the rows this collect did not
carry as gone; a collect asserting an incomplete one disables that deletion
phase exactly as an incomplete code walk does. A walk that gave up half way and
asserted completeness deletes the half it never reached.

### An edge into another graph names a family, never an instance

An edge naming neither `source_graph` nor `target_graph` is an ordinary edge of
this collect's own graph, and it is passed through untouched — including one
whose endpoint this result does not carry, because the write path resolves no
endpoint and stores both ids verbatim.

Name one of the two and the edge is a cross-graph edge: the client enumerates
that graph FAMILY's loaded graphs, locates the endpoint among them, materializes
a proxy and links the edge into the linkage graph. `target_graph` is for an edge
whose far endpoint is the destination and `source_graph` for one whose far
endpoint is the source; reversing an edge to fit the other field asserts a
different relationship. Naming BOTH fails the collect, because one resolution
reaches one family and an edge foreign at both ends names something the contract
cannot resolve.

The value is a graph type such as `code`, or a registered custom family, never
the name of a particular graph.

### The declared foreign-graph context arrives from the entry

A collector never asks for another graph's contents at run time. The operator's
registration entry declares what it needs — which graph families, which node
types, which fields, which metadata keys — and the client fills that block from
its own graphs and sends it with the call, as the input document's optional
`context` property.

The block is keyed by graph-type name, and each key holds an array of
`{graph_name, nodes[], edges[]}`. A declared family whose store held no graph is
present with an empty array; a family the entry never declared has no key at
all. Those are different facts and a collector is entitled to tell them apart. A
field the entry did not declare arrives as its zero value because it was never
sent, so an absent field means "not declared" and never "not present in the
graph".

### Every refusal is by name, and nothing is written

The client refuses rather than degrades, and the refusal names the tool, the
property or the value it is about. The classes, rather than a count:

- **Dial and handshake.** The command cannot be spawned or resolved, the process
  exits before the handshake, the session ends mid-call, the transport is
  unreachable.
- **The schema gate.** The named tool is not served, no output schema is
  advertised, either schema does not declare what the contract requires.
- **The declaration.** No declaration tool is served, or the declaration does not
  satisfy its schema. See below.
- **The call and its result.** The tool returns an error, the structured content
  does not validate, a node carries an empty type, an edge names both graph
  fields, a node or edge type falls outside the declared vocabulary.
- **The cross-graph resolution.** A named family has no loaded graph, an
  endpoint is found in none of them, an enumeration fails.
- **The registration record itself**, read before any dial: a malformed entry, an
  unknown field, a transport the client does not serve, a family that collides
  with a built-in one.

There is no count in that list on purpose. The refusals were enumerated by hand
once, and no command in this repository measures them, so a number here would be
a claim nothing checks.

## Declaring what your collector is

A collector serves a **second, required tool** beside the collect tool: a
declaration tool named `describe`. Its name is fixed rather than declared,
because the client has to know what to call before it can call anything. A
collector that does not serve it, or whose declaration does not satisfy the
declaration schema, is refused by `knowledge collector add` by name, and nothing
is written.

The declaration carries:

- `behavior`: the suggested `summarize`, `embed` and `sync` defaults, and the
  embed, summary and BM25 field lists;
- `node_type_overrides`: per-node-type overrides of those lists, keyed by node
  type;
- `node_types` and `edge_types`: the vocabularies this collector emits;
- `environment`: the environment variable NAMES this collector reads, each with
  its class — `secret`, `path` or `selector`;
- `context`: the declared foreign-graph context described above.

`knowledge collector add` calls it during the dial it already performs and
writes the entry from it: the field lists, the overrides, the environment names
and classes, the foreign context and the vocabulary all come from the
declaration rather than from the operator's typing.

**Summarize and embed are the exception, and the asymmetry is deliberate.** Both
remain operator flags, both default OFF, and the collector's suggestion is
printed in the add output and never applied. An operator flag overrides a
declared value. Everything else in the entry comes from the collector; these two
never do, because they spend money.

The declaration carries environment variable names and never their values. A
variable
declared `secret` is written into the entry in no state — not its value, and not
a reference to it either, because a reference is expanded by the process serving
the collect and reaches the collector as present-and-empty.

Once a family is registered with a vocabulary, the server refuses a collect that
emits a node or edge type outside it, by name, the way it does for a built-in
family. A family registered before declarations existed keeps accepting anything
and says so once in the collect output.

## What this module adds

Everything above is the contract. This module is 1508 lines of Go across ten
files that implement it once, so that none of it is visible to a collector:

| Symbol | What it is |
|---|---|
| `Collector[P]` | the whole surface you implement: `Tool()`, `Describe()` and `Walk()` |
| `ToolSpec`, `DefaultToolName` | the served tool's name and description; `DefaultToolName` is `collect` |
| `Result`, `Node`, `Edge` | the walk's output, in the shapes the envelope encodes |
| `Complete()`, `Incomplete(reason)` | the completeness assertion, as a type with no usable zero value |
| `ForeignContext` and its accessors | the declared foreign-graph block the walk receives |
| `ServeStdio`, `ServeHTTP` | the two entry points; `NewServer` and `NewHTTPHandler` for a host of your own |
| `InputContractJSON()`, `OutputContractJSON()`, `DescribeContractJSON()` | this module's checked-in contract bytes, for a test of your own |
| `--version` | answered by the entry point, in first argv position only, from a stamp the build injects |

Two of those are worth a sentence each.

**`Completeness` is a type rather than a `bool`** because a bool's zero value
reads as a deliberate "incomplete", so an author who forgot to think about
completeness and one who decided the walk was partial produce the same value. It
cannot be built meaningfully outside this module and `Walk` must return one, so
forgetting is a compile error. The zero literal is still writable, and it is
refused loudly when the result is encoded.

**`--version` is matched in first argv position only.** A registration entry may
carry arguments of its own and the daemon passes them through verbatim; a looser
match would eat one of them and serve nothing.

## A worked collector

The majority shape. Most collectors published here serve the default `collect`
tool and a few serve a name of their own; serve the default unless an operator
has a reason to run two providers of yours side by side.

```go
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// Params is what a collect may pass through. It infers to a JSON Schema object,
// which the framework splices into the advertised input schema. A collector with
// no parameters declares an empty struct.
type Params struct {
	Project string `json:"project"`
}

type collector struct{}

func (collector) Tool() framework.ToolSpec {
	return framework.ToolSpec{
		Name:        framework.DefaultToolName,
		Description: "collects acme issues into a knowledge graph",
	}
}

func (collector) Walk(
	ctx context.Context, id string, params Params, foreign framework.ForeignContext,
) (framework.Result, error) {
	issues, err := listIssues(ctx, params.Project)
	if err != nil {
		// A failed walk is an error, never an empty successful result: an empty
		// complete result asserts that the source holds nothing.
		return framework.Result{}, err
	}

	var nodes []framework.Node
	var edges []framework.Edge
	for _, issue := range issues {
		nodes = append(nodes, framework.Node{
			ID:      issue.Key,
			Type:    "issue",
			Summary: issue.Title,
			Status:  issue.Status,
		})
		for _, blocked := range issue.Blocks {
			edges = append(edges, framework.Edge{
				FromID: issue.Key, ToID: blocked, Type: "blocks",
			})
		}
	}
	return framework.Result{Nodes: nodes, Edges: edges, Complete: framework.Complete()}, nil
}

func main() {
	// The context every collector main installs: a cancelled walk that reported
	// a complete enumeration would arm the server's deletion phase over
	// everything it never reached.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := framework.ServeStdio(ctx, collector{}); err != nil {
		os.Exit(1)
	}
}
```

The `id` the walk receives is the collect id, which names the graph INSTANCE the
result lands in. It is not the graph family: the family is the registration
name, which the client derives on its own side and never sends, so a collector
cannot choose the graph type it writes into.

Build it as an ordinary Go binary, and stamp the version the same way the
published collectors are stamped:

```bash
go build -ldflags "-X github.com/fulminate-io/knowledge-contrib/framework.version=v1.2.3" \
  -o knowledge-collector-acme .
```

The import path in that flag must name the module being built. A wrong path is
silent: the link succeeds and leaves the default in place, so the only honest
check is running the binary and reading what it prints.

## Registering it is a config entry

There is no registration command and no server-side record. The entry IS the
registration, and removing it unregisters the collector at the next lookup.
`knowledge collector add` writes the entry for you, dialing the collector first;
writing the file by hand is equally supported and is dialed at the first
collect instead.

Two scopes, both named `collectors.json`: a user-scoped file under the knowledge
config directory, and a project-scoped one in the repository. The project scope
beats the user scope for a family both declare, whole: there is no per-field
merge across scopes.

```json
{
  "collectors": {
    "acme": {
      "type": "stdio",
      "tool": "collect",
      "command": "/opt/acme/bin/knowledge-collector-acme",
      "args": [],
      "env": {
        "ACME_REGION": "us-east-1",
        "ACME_CONFIG_DIR": ""
      }
    }
  }
}
```

The key `acme` is the graph FAMILY this collector's results land in.

**The command is resolved against the daemon's PATH, not your shell's.** A
daemon started by a service manager holds a minimal environment, so a bare
program name that works in a terminal resolves to nothing there. Write an
absolute path.

**The `env` block is the collector's whole environment, and its spellings are
load-bearing.** A name the block omits is ABSENT in the child process; a name
present with an empty value is PRESENT-and-empty, which a provider SDK reads as
a deliberate empty selection rather than as an unset variable. The two are
different inputs and the block is how you choose between them.

**No provider secret belongs in the entry**, neither a value nor a reference to
one. A reference is expanded by the process serving the collect, which under a
service manager holds almost no environment, so it reaches the collector as an
empty value that looks set. Supply a secret to the daemon's own environment
instead.

## Testing your collector

This module's own suite is the model, and the parts of it worth copying are the
ones that run a real process: a test binary that re-execs itself as a collector
over the real stdio entry point, an MCP client dialed against it over a command
transport, and comparisons made against what that child actually answers rather
than against a literal in the test.

```bash
cd framework && GOWORK=off go test ./...
```

`InputContractJSON()` and `OutputContractJSON()` return this module's checked-in
contract bytes, which is what a test of your own compares an advertised schema
against.

## Where the rest is written

The client-side guide, `docs/guides/tools/custom_collector.md` in the knowledge
repository, carries the parts that belong to the operator rather than to the
collector author: the full config-file reference, `${VAR}` expansion, the CLI,
the two transports in detail, the limits and failure behavior, and what the
post-collect tail does and does not do for a custom family.

The collectors published beside this module are worked examples of everything
above. Each carries a README of its own.
