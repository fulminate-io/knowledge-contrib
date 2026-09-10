# The knowledge collector framework, in Rust

A knowledge collector is an ordinary process that speaks MCP and answers with
nodes and edges. The daemon spawns it, calls one tool, and writes what comes back
into a graph of its own. Nothing about it is privileged: the collectors published
in this repository and a collector you write on a laptop are registered the same
way and dialed by the same code.

This crate is the Rust framework those collectors are built on. It serves the MCP
tool, advertises the contract schemas, validates the call, and encodes the result,
so a collector author writes a walk and a params type and nothing else.

The contract itself is language-neutral, and this file follows the Go framework's
README section for section so the two can be read side by side. Everything under
"The wire contract" is what a collector must do in any language; everything under
"What this module adds" is convenience this crate supplies for Rust.

It is a library. There is no install script, no release archive and no crates.io
package: you build it into your own binary and register that binary with the
knowledge client.

Apache-2.0. The licence text is at the root of the published repository; every
source file carries an SPDX header.

## The wire contract

A collector is an MCP server speaking revision 2025-06-18 or later, over stdio.
This crate pins that revision in its handshake because it is the revision the
collector contract is written against; the client asks for a newer one and
accepts the answer.

It serves one tool. That tool advertises two JSON Schema documents, and they are
not inferred from your types: they are the checked-in contract files, copied into
`contract/` and advertised verbatim, with your params schema spliced into the
input document's `params` property and nothing else touched. The top-level
required list stays `["id"]`, because the client omits `params` entirely from a
paramless collect.

The call carries the collect id, the params, and the declared foreign-graph
context. The result is an envelope of three fields — `nodes`, `edges` and
`walk_complete` — and all three are always present. `nodes` and `edges` are
always arrays and never null. A node carries `id` and `type` always and fourteen
optional fields that serialize away when empty; an edge carries its two endpoints
and its type, and at most one of `source_graph` and `target_graph`.

Stdout is the protocol stream: in stdio mode your program speaks
newline-delimited JSON-RPC on it, so any write that is not a frame corrupts the
framing and the client disconnects with an opaque handshake failure. Diagnostics
go to stderr. A corpus check scans this crate for the Rust spellings of that
defect.

## Declaring what your collector is

The contract's second, required tool is a declaration tool with a fixed name,
which the client calls to fill an operator's config entry: what environment the
collector needs and in which class, which foreign graph families it reads, and
the node and edge vocabulary its walk emits.

Your collector implements `declaration()` and this crate serves that tool for
you. The declaration is rendered and validated when the server is BUILT, not at
the first call: a collector that cannot describe itself fails to start rather
than failing the registration dial of an operator who did nothing wrong.

```rust
fn declaration(&self) -> Declaration {
    Declaration {
        behavior: Behavior { summarizable: true, syncable: true, ..Behavior::default() },
        vocabulary: Vocabulary::new(vec!["directory".into(), "file".into()],
                                    vec!["contains".into()]),
        ..Declaration::default()
    }
}
```

The rendered document is `contract/collector_describe.schema.json` verbatim, and
the crate's suite validates what it serves against that document — so a shape
this crate invented would red here rather than at a client. Four rules the type
enforces for you: the environment class is one of `path`, `selector`, `secret` or
`not-carried`; a family declaration names node types OR sets `all_node_types`,
never both; a per-node-type override names a type your vocabulary declares; and
the rendered document never carries the author-facing `reason` key, because the
client's config loader refuses an entry with a field its schema does not define.

Declaring a vocabulary is a promise the ingest path enforces: a collect carrying
a node type your declaration omits is refused, naming the node and the offending
value. Declaring nothing is a different posture, not an unfilled field — it
accepts everything and the client says so once per collect.

## What this module adds

Everything above is the contract. This crate implements it once, so that none of
it is visible to a collector:

| Symbol | What it is |
|---|---|
| `Collector` | The whole surface you implement: a tool spec, a params type, a walk |
| `ToolSpec` / `DEFAULT_TOOL_NAME` | The served tool's name and description |
| `Node` / `Edge` / `WalkResult` | The contract's vocabulary as Rust types with the contract's field names |
| `Completeness` | The walk's assertion about itself, with no default and no third state |
| `ForeignContext` | The declared foreign-graph block, keyed by graph type |
| `serve_stdio` | Serves your collector over MCP on stdin and stdout |
| `encode_result` | The envelope, and the refusals between a walk and it |
| `contract::check_tool_schemas` | The client's admission rule, so you can prove your advertisement passes before a client dials you |
| `contract::validate_result_payload` | The client's result gate, on your own side |
| `schema::input_contract_json` / `output_contract_json` | This crate's checked-in contract bytes, for a test of your own to compare against |

Two things this crate does that the Go framework delegates to its SDK, because
the Rust MCP SDK does neither: it validates the call's arguments against the
schema it advertised before anything is decoded, and it keeps the result
conforming by construction through a typed envelope with no optional fields. A
port that copied the Go doc comments without porting those checks would ship a
guarantee nothing enforces.

`WalkResult` is the one name that departs from the Go framework's, which calls it
`Result`. Shadowing the standard library's `Result` in a public API would make
every collector's signatures ambiguous to read.

## A worked collector

`src/sample.rs` is a complete collector that walks a directory into a graph of
directory and file nodes joined by `contains` edges. It reads no credential and
needs no account. `src/bin/sample-rs.rs` is its `main`:

```rust
use knowledge_collector_framework::sample::SampleCollector;
use knowledge_collector_framework::serve::serve_stdio;

#[tokio::main]
async fn main() {
    if let Err(e) = serve_stdio(SampleCollector).await {
        eprintln!("sample-rs: {e}");
        std::process::exit(1);
    }
}
```

Your own collector is the same shape:

```rust
use knowledge_collector_framework::prelude::*;

#[derive(Debug, Default, serde::Deserialize, schemars::JsonSchema)]
struct Params {
    /// The directory to walk.
    root: String,
}

struct Files;

impl Collector for Files {
    type Params = Params;

    fn tool(&self) -> ToolSpec {
        ToolSpec::new("collect", "walk a directory into a graph")
    }

    fn walk(
        &self,
        id: &str,
        params: Params,
        _foreign: &ForeignContext,
    ) -> Result<WalkResult, WalkError> {
        let node = Node::new(params.root.clone(), "directory");
        Ok(WalkResult::new(vec![node], vec![], Completeness::complete()))
    }
}
```

The `id` your walk receives is the collect id. It names the graph INSTANCE the
result lands in, not the graph family: the family is the registration name, which
the client derives on its own side and never sends.

Every walk returns a completeness assertion. `Completeness::complete()` says the
walk enumerated the whole source; `Completeness::incomplete(reason)` says it did
not, and carries the reason. It is not cosmetic: a complete collect lets the
server treat the rows this collect did not carry as gone. There is no default and
no third state, so a walk that never decided does not compile. The sample's
`max_depth` parameter exists to show it.

An `Err` from your walk becomes a tool error carrying your message and naming
your collector; it never becomes an empty successful result. A walk that PANICS
is contained and reported the same way rather than killing the provider mid-call.
The sample's `demo_panic` and `demo_undeclared_node_type` parameters exist so
those arms can be driven from the client. No production collector should carry
one.

Build it:

```bash
cargo build --release --locked
```

The binaries land in `target/release/`. The crate stands alone: it is not a
workspace member and it joins no workspace. It builds on Rust 1.88 and newer;
that floor is the SDK's, and the crate's own suite reads it out of the SDK's
manifest rather than trusting this sentence.

## Registering it is a config entry

```
knowledge collector add --tool collect my-collector -- /abs/path/to/my-collector
```

`collector add` dials the binary, handshakes, lists its tools and runs the schema
gate before it writes the entry, so a collector that cannot be registered has told
you why at that point rather than at the first collect.

The client stores the command verbatim and resolves it with a single path lookup
in the process that serves the collect. That process is the daemon, not your
shell, so a bare name resolves against the DAEMON's `PATH` and fails with "is not
executable" if it is not on it. An absolute path is the recommendation for that
reason. It is not a requirement the client enforces, and putting `PATH` in the
entry's environment block does not help, because the block is applied after the
lookup.

The entry's environment block is the child's WHOLE environment: a name the block
omits is absent however the daemon is configured, and a name it carries with an
empty value arrives present and empty. Nothing of the daemon's own environment
leaks through.

Then collect:

```
knowledge collect '{"type":"my-collector","id":"probe","params":{"root":"/some/dir"}}'
```

Your walk's third parameter is what the entry declared this collector needs, read
out of the operator's own graphs and sent with the call. A declared family that
matched no graph is PRESENT with an empty list; a family the entry never declared
has no key at all. `ForeignContext::graphs` returns an `Option` so the two are
distinguishable, because they are different facts.

## Testing your collector

```bash
cargo test --locked -- --nocapture
```

The parts of this crate's suite worth copying are the ones that run a real
process: a binary spawned over the real stdio entry point, an MCP client driving
it over that transport, and comparisons made against what the child actually
answers rather than against a literal in the test.

`schema::input_contract_json()` and `output_contract_json()` return this crate's
checked-in contract bytes, which is what a test of your own compares an advertised
schema against, and `contract::check_tool_schemas` is the client's own admission
rule so you can prove your advertisement passes without a client.

`--nocapture` is not decoration: several tests are pins on work landing in sibling
changes, and each SKIPS BY NAME while its artifact is absent, printing the change
it waits for and the assertion it will then make. Without that flag the harness
swallows those lines on a passing run.

There are two binaries. `sample-rs` is the worked collector above.
`conformance-stub` is test-only: it implements the sixteen provider-misbehaviour
modes the client's own contract tests assert against, several of which advertise
deliberately non-conforming schemas or exit at a named point. Do not register it
as a collector.

## Where the rest is written

`cmd/collectors/framework/README.md` in the knowledge repository is this file's
Go counterpart and carries the contract's own prose at length.

The client-side guide, `docs/guides/tools/custom_collector.md`, carries the parts
that belong to the operator rather than to the collector author: the full
config-file reference, `${VAR}` expansion, the CLI, the two transports in detail,
the limits and failure behavior, and what the post-collect tail does and does not
do for a custom family.
