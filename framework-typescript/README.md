# The knowledge collector framework, in TypeScript

A custom collector is a program the knowledge daemon runs. It speaks MCP over
stdin and stdout, serves one tool, and answers a call with a graph: nodes, edges,
and one assertion about whether it enumerated the whole source. This package is
the serving and contract layer under that program, so what you write is a walk.

It is a library. There is no install script, no npm publish and no release
archive: you build it from source in your own tree and register the compiled
script yourself.

## The wire contract

The contract is the same one the Go framework serves, because it is the same two
files. A collector in any language is held to it by the client's own registration
gate and again at every collect.

### Two checked-in JSON Schema files are the contract

`contract/collector_input.schema.json` and `contract/collector_output.schema.json`
are copies of the client's own files. They are checked in rather than generated
because a collector author reads them, and this package advertises them rather
than building a schema from its own types. A test in this package compares its
copies byte for byte against the Go framework's, which sits beside it in this
tree and in the published one alike.

### The vocabulary is 16 node fields and 9 edge fields, of which 14 and 6 are optional

A node carries `id` and `type` and may carry `symbolName`, `filePath`,
`language`, `startLine`, `endLine`, `content`, `signature`, `summary`,
`description`, `source`, `status`, `keywords`, `isExported` and `metadata`. An
edge carries `fromId`, `toId`, `type` and may carry `weight`, `confidence`,
`method`, `evidence`, `sourceGraph` and `targetGraph`. The properties are
camelCase because that is TypeScript; this package emits each under the
contract's own key, so `symbolName` reaches the wire as `symbol_name`. Anything
outside the vocabulary rides in `metadata`, exactly as the built-in collectors do.

Timestamps and collect bookkeeping are deliberately absent. The write path stamps
them, so a collector cannot.

### Three wire-shaping rules a port has to reproduce

1. The advertised **output** schema is the contract file verbatim. A schema built
   from a nullable list infers a union type, and the client's comparator reads a
   scalar one, so a constructed schema is refused with a message about a type the
   author never wrote.
2. The advertised **input** schema is the contract file with only `params`
   replaced by your own declaration. Building the whole document instead puts
   `params` on the top-level required list, and the client omits `params` from
   the call entirely when a collect carries none, so every paramless collect
   would be refused before it was sent.
3. Every list is an **array**, never null and never an omitted key. A walk that
   found nothing is the first real run of a new collector, so it is the cell this
   hits.

`encodeResult` does all three. Nothing upstream will do them for you: measured,
an MCP SDK server passed a result carrying `edges: null` straight through.

### An incomplete walk says so, and that is what protects the graph

`walk_complete` rides to the server's deletion guard. A collect asserting a
complete walk lets the server treat the rows this collect did not carry as gone.
So `complete` has no usable zero value: build it with `complete()` or
`incomplete(reason)`, and a result carrying anything else is refused when it is
encoded, naming your tool. The reason is for your own logs; the wire carries a
boolean.

### An edge into another graph names a family, never an instance

Set `targetGraph` and the client resolves `toId` against that graph family and
links the edge into the linkage graph. `sourceGraph` is its mirror, for a
relationship whose far endpoint is the source. Set at most one: an edge foreign
at both ends names something the contract cannot resolve.

### The declared foreign-graph context arrives from the entry

A collector never asks for the context block at run time. The operator's config
entry declares which graph families, node types and fields it needs, and the
client fills the block before the call. Your walk receives a `ForeignContext`
with `isEmpty()`, `families()`, `graphs(family)` and `except(...families)`. A
declared family that matched no graph is present and empty; one the entry never
named is absent, and the two are different facts.

### Every refusal is by name, and nothing is written

Arguments that do not satisfy the advertised input schema never reach your walk.
A node with an empty type, an unasserted completeness value and an incomplete
walk with no reason are each refused when the result is encoded, naming your tool
and the offending index. A walk that throws becomes a tool error with text, never
an empty successful result.

## Declaring what your collector is

```ts
import { complete, type Collector, type ForeignContext, type Result } from "knowledge-collector-framework";

interface Params { root?: string }

class MyCollector implements Collector<Params> {
  tool() {
    return { name: "collect", description: "Walks a directory." };
  }
  paramsSchema() {
    return {
      type: "object",
      properties: { root: { type: "string" } },
      required: ["root"],
      additionalProperties: false,
    };
  }
  walk(id: string, params: Params, foreign: ForeignContext): Result {
    return { nodes: [{ id: params.root!, type: "directory" }], complete: complete() };
  }
}
```

The params schema is declared rather than inferred, and that is the one place
this surface cannot mirror the Go framework's: Go reflects over the params type
at run time, and TypeScript erases its types at compile time. It must declare
`type: "object"`, because the contract does and the client's gate compares it.

## What this module adds

- The stdio speaker. This package speaks newline-delimited JSON-RPC itself and
  takes no MCP SDK dependency. The SDK route was measured and rejected: it
  validates neither call arguments nor results, not even the contract's own
  required `id`, so its contribution is framing alone at a cost of 86 further
  packages in a lockfile that ships.
- Argument validation before the walk, against the schema you advertised.
- Result validation after it, against the contract's output schema.
- The three contract gates the client will run against you — `checkToolSchemas`,
  `validateResultPayload` and `decodeResult` — exported, so your own tests can
  run them locally instead of discovering a refusal at registration.

## A worked collector

`examples/sample-collector.ts` is a credential-free collector over a directory
tree. It emits one node per file and per directory and a `contains` edge for each
entry, and it asserts an incomplete walk, naming the path, for any subtree it
could not read. Read it before writing your own: the completeness arm is the part
worth copying.

## Building it

```sh
npm ci
npm run build
npm test
```

`npm ci` installs exactly what the committed `package-lock.json` resolves and is
the only step that reaches the network. The build emits plain JavaScript under
`dist/`, and `dist/examples/sample-collector.js` is what you register.

**Where the install and the build output live.** Both are inside this directory:
`node_modules/` and `dist/`. Both are named in `.gitignore`, so neither is ever
committed, and a clone therefore carries neither. That matters beyond tidiness:
the publish copies this directory whole and consults no ignore file, so anything
left here by a local build would be published. Nothing is, because the tree the
publish stages from is a fresh checkout. Delete both freely; `npm ci` and
`npm run build` rebuild them.

**stdout is the protocol stream.** Every byte your collector writes there must be
a JSON-RPC message. `console.log` writes to stdout and will corrupt the framing;
an operator sees an opaque handshake failure with nothing to go on. Write
diagnostics with `console.error` or `process.stderr.write`.

## Registering it is a config entry

```sh
knowledge collector add --tool collect sample-ts -- /opt/node/bin/node /srv/collectors/framework-typescript/dist/examples/sample-collector.js
```

That writes an entry into `collectors.json`:

```jsonc
{
  "collectors": {
    "sample-ts": {
      "type": "stdio",
      "command": "/opt/node/bin/node",
      "args": ["/srv/collectors/framework-typescript/dist/examples/sample-collector.js"],
      "env": { "HOME": "/srv/collectors" },
      "tool": "collect"
    }
  }
}
```

The interpreter is the `command` and the compiled script is an `args` element.
Each argument is its own array element, never a shell string.

### Give `command` an absolute path unless you know the name is on the daemon's PATH

This is a recommendation, not a requirement the client enforces. The client
stores whatever you type after `--` verbatim, and the daemon resolves `command`
once, with a single path lookup, **in the process serving the collect**. That
process is not your shell: a daemon started by a service manager does not inherit
your login shell's environment, and under launchd its `PATH` is
`/usr/bin:/bin:/usr/sbin:/sbin`. A `node` installed under a user prefix does not
resolve there, and the collect fails with `is not executable`.

`knowledge collector add` proves nothing about this, because its dial runs in
your shell with your `PATH`. Putting `PATH` in the entry's `env` block does not
help either: the block is the child's environment and is applied after the lookup
has already happened. An absolute path sidesteps all of it, and `command -v node`
prints the one to use.

### The env block is the child's whole environment

Names you list are what the collector sees, with the values you give; a name you
omit is absent however the daemon is set, and a name set to the empty string is
present and empty. There is no inheritance. So the block carries configuration,
not credentials: a credential reaches a collector the way that collector
documents, commonly a file under the `HOME` you declare.

## Testing your collector

Run the three contract gates over your own advertised schemas and your own
result, in your own suite:

```ts
import { checkToolSchemas, decodeResult } from "knowledge-collector-framework";

checkToolSchemas("collect", tool.inputSchema, tool.outputSchema);
decodeResult("collect", result.structuredContent);
```

Both throw, naming the tool and the offending property, which is the same message
the client would give you at registration.

## Where the rest is written

The collector contract, the config file's scopes, the `${VAR}` expansion rules
and the behavior block are in `docs/guides/tools/custom_collector.md` in the
knowledge repository. The Go framework beside this one serves the same contract
and its README covers the same ground for Go.
