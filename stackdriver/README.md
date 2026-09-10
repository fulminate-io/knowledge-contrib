# Google Cloud Logging collector

Reads a Google Cloud project's log entries and returns them as a graph: clustered
message templates, label streams, compressed entry chunks and shared labels.

It is a standalone program that speaks MCP on stdin and stdout. The knowledge
daemon spawns it, calls one tool, and writes what it returns into a graph of its
own. Nothing about the daemon is compiled into it, and it imports none of the
daemon's code.

## Installing it

Released builds live in the public **knowledge-contrib** repository: every
release there carries one archive per collector per platform, named
`knowledge-collector-stackdriver-<os>-<arch>.tar.gz` (`.zip` on Windows) and holding a
single binary called `knowledge-collector-stackdriver`, plus a `checksums.txt` covering
all of them. The install script in that repository is the way to get one: it
picks the archive for your platform, verifies it against `checksums.txt` and
installs nothing if the checksum is missing or does not match. Building from
source stays supported and is described below.

Install or build the binary and name it in a collector config entry:

The one command is:

```bash
curl -fsSL https://raw.githubusercontent.com/fulminate-io/knowledge-contrib/main/install.sh | sh -s -- stackdriver
```

It takes `--version <tag>` to pin a release, and reads a GitHub token from
`GH_TOKEN`, then `GITHUB_TOKEN`, when the release is private; a public release
needs neither, and the token is never written to the entry, to the version
sidecar or to any other file.

**It writes no provider credential VALUE into the entry.** For a credential name
your installing shell holds it writes a REFERENCE — `"NAME": "${NAME}"` — and the
process that serves the collect reads the value out of your own environment when
it starts this collector; a name that shell does not hold is left out of the
entry entirely. It never writes the `${NAME:-}` form: that one is expanded by the
serving process too and resolves to an empty value the provider sees as set,
which is a different failure and not a working credential. The bare form has no
such default, so a serving process without the name refuses that entry by name
and leaves every other collector in the file collecting. What the script also
writes is the non-secret configuration and the paths below, and a credential
reaches this collector the way the entry above shows.

Reversing it removes the binary, its version sidecar and the config entry, and
no graph:

```bash
curl -fsSL https://raw.githubusercontent.com/fulminate-io/knowledge-contrib/main/uninstall.sh | sh -s -- stackdriver
```

Or write the registration yourself.

Build the binary and name it in a collector config entry:
```
go build -o knowledge-collector-stackdriver .
```

Registration is a config file: `~/.knowledge/collectors.json` for this machine's
operator, or `<repository root>/.knowledge/collectors.json` for one project.
`knowledge collector add` writes the entry for you:

```
knowledge collector add --tool collect stackdriver -- /home/you/.knowledge/bin/knowledge-collector-stackdriver-collector
```

Options come before the name and the command follows the `--`, mirroring
`claude mcp add`. Repeat `-e KEY=VALUE` for each environment name the entry must
declare, and add `--summarizable=true` / `--embeddable=true` to opt into LLM
summaries and vectors. The entry's NAME is the graph family the results land in;
the collect id is the instance inside it. The same entry written by hand:

```jsonc
{
  "collectors": {
    "stackdriver": {
      "type": "stdio",
      "command": "/home/you/.knowledge/bin/knowledge-collector-stackdriver",
      "context": {
        "aws": {
          "node_types": [
            "cloud-resource"
          ],
          "node_fields": [
            "id",
            "symbol_name"
          ],
          "metadata_keys": [
            "name",
            "service"
          ],
          "edge_fields": [
            "from_id",
            "to_id"
          ]
        },
        "azure": {
          "node_types": [
            "cloud-resource"
          ],
          "node_fields": [
            "id",
            "symbol_name"
          ],
          "metadata_keys": [
            "name",
            "service"
          ],
          "edge_fields": [
            "from_id",
            "to_id"
          ]
        },
        "k8s": {
          "node_types": [
            "cloud-resource"
          ],
          "node_fields": [
            "id",
            "symbol_name"
          ],
          "metadata_keys": [
            "name",
            "service"
          ],
          "edge_fields": [
            "from_id",
            "to_id"
          ]
        }
      },
      "tool": "collect",
      "env": {
        "GOOGLE_APPLICATION_CREDENTIALS": "/home/you/.config/gcloud/application_default_credentials.json",
        "GOOGLE_CLOUD_PROJECT": "my-project",
        "HOME": "/home/you"
      },
      "behavior": {
        "summarizable": true,
        "embeddable": true,
        "syncable": true
      }
    }
  }
}
```

### The `context` block, and what each entry is for

The `context` key is keyed by graph family: the name a cloud inventory collector
is registered under. It is generated — a test renders it from this collector's
own declaration and asserts the README carries it — so the documented block and
what the collector can receive cannot drift. The client fills exactly what is
declared and nothing else, and it refuses a family this daemon has no collector
registered for, naming it and listing the ones it can supply, so delete the
entries for providers you do not run.

All three entries serve **EMITTED_BY** and confirmed **CORRELATES_WITH**. A log
stream's service-identifying label is resolved to a cloud resource by NAME, and
the three values a resource can be matched under are its `symbol_name` and its
`name` and `service` metadata keys — which is exactly what the entries ask for.
An undeclared field arrives absent rather than empty, so a block missing those
keys indexes every candidate under its symbol name alone and resolves nothing.
The `from_id` / `to_id` edge fields are what CONFIRM a temporal correlation
between two templates. The resolutions are computed whatever `correlation` is
set to, so this block is load-bearing for the proxy half even with correlation
turned off. `cloud-resource` is the one node type each of those inventory
collectors emits.

**`gcp` is deliberately absent, and it is the one you would expect here.** The
gcp inventory collector emits its resource type AS the node type — forty-nine
`gcp:<service>:<kind>` values plus a `gcp-cidr-block` sentinel — and a
declaration selects a type only by naming it exactly, at one node read per type
per graph per collect. The family waits for an explicit family-level selector
rather than shipping a forty-nine-entry list or a key that selects nothing.

There is no `graph` key and no `reason` key. The family is the map key, and the
loader refuses an unknown field by name — which fails the whole config file, not
just the entry that carried it.

The `env` block above is ABBREVIATED, and an abbreviated block is a working
collector only when the three names it lists are the three your host needs. The
entry names the command, the tool, and the environment the child runs under.
A stdio collector's environment is exactly its entry's `env` block: the daemon
copies nothing from its own environment and adds nothing, so a variable the entry
does not name is absent from the child however the daemon was started. The
complete list for your operating system is what `declaredEnvNames` returns, and
the five groups it covers are named in the section below.

## Credentials

Application Default Credentials, and nothing else. There is no service-account
path parameter and no inline credential parameter, so no operator secret lives in
the config file. ADC looks in three places, in order:

1. `GOOGLE_APPLICATION_CREDENTIALS`, if set, naming a key file.
2. The gcloud well-known credential file under the home directory.
3. The metadata server, when running on Google infrastructure.

A collector whose entry names none of the variables those paths need cannot find
a credential, and the error says so, naming Application Default Credentials
rather than blaming the network. A failure to REACH the endpoint reports itself
as a transport failure instead, because the two have different remedies.

The collector needs read access to the project's logs and nothing more.

## The environment its entry declares

Leaving a name out of the entry does not disable a feature; it makes this one
client behave differently from every other Google client on the same host, with
no error saying why. So the entry names every variable this collector's
dependencies read, in five groups:

- **Credential discovery** — `GOOGLE_APPLICATION_CREDENTIALS`,
  `GCE_METADATA_HOST`, `GOOGLE_CLOUD_PROJECT`, `GOOGLE_CLOUD_QUOTA_PROJECT`, and
  `HOME` (`APPDATA` on Windows).
- **Proxies** — `HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY` and their lowercase
  spellings.
- **Trust roots** — `SSL_CERT_FILE`, `SSL_CERT_DIR`. Read on Unix-like systems
  whose CA bundle is not at a compiled-in default: a container image, or a host
  behind an inspecting proxy.
- **Auth chain selection** — `GOOGLE_API_GO_EXPERIMENTAL_DISABLE_NEW_AUTH_LIB`
  and its `ENABLE` counterpart.
- **Endpoint and transport** — `GOOGLE_CLOUD_UNIVERSE_DOMAIN`,
  `GOOGLE_API_USE_CLIENT_CERTIFICATE`, `GOOGLE_API_CERTIFICATE_CONFIG`,
  `GOOGLE_API_USE_MTLS_ENDPOINT`, `GOOGLE_API_USE_MTLS`,
  `EXPERIMENTAL_GOOGLE_API_USE_S2A`, `S2A_TIMEOUT`,
  `GOOGLE_AUTH_TRUST_BOUNDARY_ENABLED`.

Some names are deliberately left out, each at a cost written down in `env.go`.
`S2A_ACCESS_TOKEN` is credential material rather than a switch, so a host that
expects an S2A token from the environment falls back to ordinary TLS here. The
xDS bootstrap pair and the gRPC and runtime debug knobs are excluded because a
collector whose transport an entry can reconfigure is a collector whose failures
are not reproducible from the entry alone.

## The tool

One tool, `collect`. It takes the collect id, which names the graph instance the
result lands in, and a `params` object:

| Field | Meaning |
|---|---|
| `project` | The Google Cloud project to read. Required. |
| `filter` | A Cloud Logging advanced filter expression, appended verbatim. |
| `log_name` | One log to read, short (`stderr`) or fully qualified. |
| `start`, `end` | RFC 3339 bounds. Either may be omitted. |
| `severity_min` | One of TRACE, DEBUG, INFO, WARN, ERROR, CRITICAL. |
| `text_filter` | Keep entries whose message contains this text. |
| `fields` | Exact-match label filters. |
| `max_entries` | Bound on entries read, positive. Omit it to read the whole range. |

Every value except `filter` is sanitized and escaped before it enters the filter
string. `filter` is passed through untouched, because a caller asking for a
native filter expression is asking for exactly that.

**There is no cap.** Omitting `max_entries` reads the whole time range, and a
bound the caller names is honored at any size with no ceiling above it. The
traffic between the daemon and a collector is MCP to MCP and none of it reaches a
language model's context, so the reason a cap exists elsewhere does not apply; a
module-side default would drop collected data for a caller who asked for none of
it.

The three inputs are distinct. **Absent** means read everything. A **positive**
value is honored as given. **Zero or negative is refused**, naming what was sent:
a bound says how many entries to read, and neither of those says a number, so
reading an explicit `0` as "everything" would be the widest possible reading of
the value that most plainly means the opposite. An explicit `null` is JSON's own
spelling of no value and reads as absent.

When a bound the caller set does stop the read before the source is exhausted,
the result says so, and the daemon then declines to treat rows this collect did
not carry as deleted. Whether anything remained is measured by looking past the
bound rather than inferred from reaching it.

## What it emits

| Node | Id | Carries |
|---|---|---|
| `log-template` | Hash of the clustered pattern | The pattern, severity, count, first and last seen |
| `log-stream` | Fingerprint of the full label set | Every label, and a fingerprint over the shared ones |
| `log-chunk` | Hash of stream, template and time window | The compressed entries for one five-minute bucket |
| `log-label` | `log-label:key=value` | One shared node per low-cardinality label pair |
| `proxy` | `proxy:cloud:<account>:<resource>` | A pointer to a resource in a cloud graph |

Edges: `HAS_LABEL` from a stream to each shared label, `BELONGS_TO` from a chunk
to its stream, `CONTAINS` from a template to its chunks, `EMITTED_BY` from a
label to a cloud resource, and `CORRELATES_WITH` between two templates whose
errors burned together in services that actually depend on each other.

**Every id is derived, never generated.** That is what makes a second collect
over the same window reconcile against the first instead of duplicating it, and
it is why the message masking, the label set and the chunk window are all fixed
rules rather than tunables.

A chunk's payload rides in `content` as base64 of a zstd frame, and the chunk
says so in its `content_encoding` metadata. The envelope is JSON and a JSON
string cannot carry arbitrary bytes without being silently rewritten, so the
encoding is what makes the payload survive the trip rather than a preference.

## Cloud-linked output

The proxy nodes, the `EMITTED_BY` edges and the `CORRELATES_WITH` edges all rest
on knowing which log label names which cloud resource, and whether two resources
depend on each other. Neither is a fact about logs, and this process cannot read
the graph that holds them: it has its tool arguments and its declared environment
and nothing else. Those answers arrive as declared foreign-graph context on the
collect input, which the entry asks for and the daemon fills.

To get them, the entry declares the cloud graphs it needs, with each node's `id`,
`symbol_name` and `metadata`, and the edges between them. A log label resolves to
a declared node when the label's value equals that node's `symbol_name`, or its
`metadata.name`, or its `metadata.service`. Matching is exact: a fuzzy rule would
resolve a label to a resource that merely resembles it, and every resolution
becomes an edge asserting that this log stream came from that cloud resource, so
a wrong one is worse than a missing one. Two error templates in different
services correlate only when the declared slice carries an edge between their two
resources.

**Without that context the collector emits none of the three, and the collect
succeeds.** That is the honest result rather than a degraded one: nothing was
dropped, no read failed, and no resolution was attempted and abandoned. There is
simply nothing to resolve against.

The proxy is a node in **this** collector's own graph that points at a cloud
resource through its metadata, and the edge to it is an ordinary in-graph edge.
It is not a cross-graph edge and sets no target graph.

## What it declares about itself

This collector serves the contract's required `describe` tool: one document
naming its suggested behavior and field lists, its five node types and its five edge types, the environment
variables it reads with what an installer must do with each, and
nothing else.

`knowledge collector add` calls it once while it dials, and **writes the entry
from the answer** — so the block above is what an install produces rather than
what you transcribe. Two consequences worth knowing:

- **The node and edge types it declares are CLOSED once registered.** A collect
  carrying a type outside them is refused at ingest, by name, and nothing is
  written. That is what makes the vocabulary worth reading.
- **Summarizing and embedding stay yours.** The declaration SUGGESTS them and
  `collector add` prints the suggestion; what is written comes from your
  `--summarize` and `--embed` flags alone.

Its binary answers two of the same questions on argv, which is how the installer
builds its table without speaking MCP:

```bash
knowledge-collector-stackdriver --describe-tool        # the tool name a config entry's `tool` field takes
knowledge-collector-stackdriver --describe-env-table   # one `class name` row per declared variable
```

## Running the tests

```
cd cmd/collectors/stackdriver && go test ./...
```

No test reaches the network or needs a credential. The provider arm spawns the
test binary as a real stdio child and speaks JSON-RPC to it over its own pipes,
so the served surface is exercised end to end offline.
