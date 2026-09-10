# framework-python

The Python port of the knowledge custom-collector framework. Write a walk; this
serves it as an MCP tool over stdio, advertises the collector contract's schemas,
validates the call's params before the walk runs, and encodes the result as the
contract envelope.

It takes no runtime dependency. Everything here is the standard library, so
copying the source into your project is the whole install and running its tests
is one interpreter invocation.

## Install from source

There is no package to install and no index to reach. Copy the package directory
into your project, or clone this repository and point at it:

```sh
git clone https://github.com/fulminate-io/knowledge-contrib.git
cd knowledge-contrib/framework-python
python3 -m unittest discover
```

`knowledge_collector/` is the package. `pyproject.toml` records the metadata and
the Python version floor; it declares no dependencies, and nothing here installs
anything.

## The worked sample

`sample_collector.py` walks one local directory into `directory` and `file` nodes
joined by `contains` edges. It reads no credential and opens no network
connection.

```python
from knowledge_collector import Collector, Complete, Edge, Node, Result, ToolSpec, main

class DirectoryCollector(Collector):
    def tool(self):
        return ToolSpec(name="collect", description="Walk one local directory.")

    def params_schema(self):
        return {"type": "object", "required": ["path"],
                "properties": {"path": {"type": "string"}}}

    def describe(self):
        ...          # the declaration: behaviour, env names, vocabulary

    def walk(self, collect_id, params, foreign):
        ...          # return Result(nodes, edges, Complete())

if __name__ == "__main__":
    import sys
    sys.exit(main(DirectoryCollector()))
```

Four things the framework decides for you and one it does not:

- **The advertised schemas.** The output schema is the contract file verbatim; the
  input schema is the contract file with your `params_schema()` spliced in, so the
  top-level required list stays `["id"]`.
- **The params gate.** A call whose arguments do not satisfy the schema you
  advertised never reaches `walk()`.
- **The envelope.** Empty lists serialize as `[]` and never `null`;
  `walk_complete` is always present.
- **The refusals.** A node with an empty type, a walk that asserts nothing, an
  `Incomplete` with no reason and an empty collect id are each refused by name.
- **The completeness assertion is yours.** `Result` takes it as a required
  argument: return `Complete()` when the walk enumerated the whole source, or
  `Incomplete(reason)` when it did not. An incomplete collect disables the
  server's deletion phase, so asserting completeness for a walk that gave up half
  way is not a cosmetic error.

Raise from `walk()` when the source cannot be read. That becomes a tool error
carrying your message, which the client treats as a refused collect that writes
nothing. Returning an empty complete result instead would assert that the source
exists and holds nothing.

## Register it

`knowledge collector add` takes the interpreter as the command and the script as
an argument:

```sh
knowledge collector add --tool collect sample-py -- \
    /usr/bin/python3 /abs/path/to/sample_collector.py
```

`add` dials the provider before it writes anything: it completes the handshake,
lists the tools, finds the one you named and runs the schema gate against it. A
provider that fails any of those is refused at registration rather than at the
first collect.

Then collect:

```sh
knowledge collect --type sample-py --id my-graph --params '{"path":"/abs/path"}'
```

### The interpreter path

**Give `command` an absolute path unless you know the name is on the daemon's own
`PATH`.** This is a recommendation with a mechanism behind it, not a requirement
the client enforces: the client stores whatever you write after `--` verbatim and
resolves it with a single lookup **in the process that serves the collect**, which
is the daemon rather than your shell. A bare `python3` therefore resolves against
the daemon's `PATH`, and a name that is not there fails with "is not executable".

Putting `PATH` in the entry's environment block does not help, because the lookup
has already happened by then.

### The environment block

**The entry's env block is the child's whole environment.** A collector spawned
from an entry receives exactly the names that entry declares and nothing else — no
`PATH`, no `HOME`, none of the daemon's own variables — so anything your walk reads
from the environment has to be declared:

```sh
knowledge collector add --tool collect sample-py \
    -e SAMPLE_PY_ROOT=/srv/data -- /usr/bin/python3 /abs/path/to/sample_collector.py
```

A name the block omits is absent however the daemon is configured. A name it
carries with an empty value arrives present and empty, which is a different thing
from absent. Credentials reach a collector this way and only this way; this
framework reads nothing from a file.

## The describe tool

Every collector implements `describe()`. It returns the declaration the framework
serves under a fixed tool name: your collect tool's name, the behaviour defaults
for the graph, per-node-type overrides, the environment names you read with each
name's class (`path`, `selector` or `secret`, names only and never values), the
foreign-graph context you need, and the node and edge vocabulary your walk emits.

The declaration is required rather than optional, and the reason is that an empty
vocabulary is a claim rather than an absence: a framework that filled one in for
you would be saying something on your behalf that you never wrote.

## Diagnostics go to stderr

On stdio, **stdout is the protocol stream**. Anything else written there corrupts
the JSON-RPC framing and reaches an operator as an opaque handshake failure with
nothing pointing at the print that caused it. Write diagnostics to stderr; the
client surfaces a failing provider's stderr verbatim.

## What is here

| Path | What it is |
| --- | --- |
| `knowledge_collector/` | The library. |
| `knowledge_collector/contract/` | Byte copies of the contract schemas the client checks against. |
| `sample_collector.py` | The worked example above. |
| `conformance_stub.py` | A test-only provider serving the named shapes the client's own contract tests drive. Not a collector. |
| `tests/` | The port's suite. `python3 -m unittest discover`. |

## Running the tests

```sh
cd framework-python
python3 -m unittest discover
```

No install, no index, no network. Some tests read the knowledge client's own
checked-in files to compare bytes against them; outside a checkout that carries
them, those tests skip and say what they looked for.
