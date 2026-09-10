# knowledge-collector-loki

A knowledge custom collector for [Grafana Loki](https://grafana.com/oss/loki/).
It reads a window of logs over Loki's HTTP API and returns a log graph: the
recurring message patterns, the label sets that produced them, the compressed
entries themselves, and the shared labels that join them.

It serves one MCP tool over stdio. The knowledge daemon spawns it from an entry
in the collector config file and calls that tool; the collector talks to Loki
and to nothing else.

## What it produces

Four node types and three edge types.

| Node | What it is |
|---|---|
| `log-template` | A clustered message pattern, with the variable parts replaced by `<*>`. Carries the pattern, the highest severity observed, the match count and the time range. |
| `log-stream` | One distinct label set. Its id is a hash of the whole label set, so the same source keeps the same node between collects. |
| `log-chunk` | A compressed block of entries sharing one stream, one template and one five-minute window. |
| `log-label` | One shared `key=value` pair, hoisted out so streams that share it point at one node. |

| Edge | From | To |
|---|---|---|
| `HAS_LABEL` | a stream | one of its labels |
| `BELONGS_TO` | a chunk | its stream |
| `CONTAINS` | a template | a chunk of its entries |

When the collect carries a declared cloud context block, the walk also emits a
`proxy` node per resolved cloud resource, an `EMITTED_BY` edge from the log
label to it, and a `CORRELATES_WITH` edge between two error templates whose
owning services burned at the same time and whose resources the block says
depend on each other. All of them are ordinary nodes and edges of this collect's
own graph; the proxy's cross-graph reference rides its id, its source and its
metadata.

## Linking log labels to cloud resources

The collector holds no cloud session and never queries one. What it reads is the
**context block**: the cloud graphs your config entry declares it needs, which
the knowledge client fills from your own graphs and sends with the collect.
Declare nothing and the block is empty, nothing resolves, and none of those
families is emitted.

A label resolves to a cloud resource when all of this holds.

- The label is **low cardinality** for this collect. A label naming a pod or a
  container names an instance, and the cloud graph holds services.
- Its key is one of `service`, `namespace`, `deployment` or `app`, and its value
  is not empty.
- Some declared cloud node **is** named that value, exactly. A node is named by
  its symbol name, its `name` metadata or its `service` metadata, in that order.
  Matching on more than one of them is not matching more than once.
- **The first declared graph carrying such a node wins**, and the first such node
  within it. Its graph name is the account the proxy is stamped with, and the
  node's id is the resource. Declaration order, not alphabetical: the block is
  built by the knowledge client, which already sorts the graph names before
  filling it, so on a real block the two orders agree and on one you wrote by
  hand the order is yours.

Two streams naming one service resolve once, so one resource is one proxy node
with an edge from each label. A label matching nothing is skipped: your
declaration decides what was fetched, and a label naming something outside it is
an ordinary label.

## Correlating errors across services

Two services logging errors at the same moment is a coincidence until something
says their resources are actually connected. The collector pairs error templates
owned by different services whose time ranges overlap, and emits an edge only for
a pair the **declared block's own edges** connect. Declare the cloud family
without its edges and you get the candidates' proxies and no correlation edges,
which is the honest answer rather than a degraded one.

Only templates at `ERROR` or above are paired, and two resources in different
accounts are never connected: the declared edges are within one graph by the
block's shape.

The detection and the edge itself come from a module every knowledge logs
collector shares, so the edge one collector writes is the edge the others write.

## Installing it

Released builds live in the public **knowledge-contrib** repository: every
release there carries one archive per collector per platform, named
`knowledge-collector-loki-<os>-<arch>.tar.gz` (`.zip` on Windows) and holding a
single binary called `knowledge-collector-loki`, plus a `checksums.txt` covering
all of them. The install script in that repository is the way to get one: it
picks the archive for your platform, verifies it against `checksums.txt` and
installs nothing if the checksum is missing or does not match. Building from
source stays supported and is described below.

Install or build the binary and write an entry for it in your collector config file. The

The one command is:

```bash
curl -fsSL https://raw.githubusercontent.com/fulminate-io/knowledge-contrib/main/install.sh | sh -s -- loki
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
serving process too and resolves to an empty value, which this collector refuses
by name. The bare form has no such default, so a serving process without the name
refuses that entry by name and leaves every other collector in the file
collecting. What the script also writes is the non-secret configuration below.

Reversing it removes the binary, its version sidecar and the config entry, and
no graph:

```bash
curl -fsSL https://raw.githubusercontent.com/fulminate-io/knowledge-contrib/main/uninstall.sh | sh -s -- loki
```

Or write the registration yourself.

Build the binary and write an entry for it in your collector config file. The
family name is the entry's name, and it becomes the graph type the collect
writes into.

Registration is a config file: `~/.knowledge/collectors.json` for this machine's
operator, or `<repository root>/.knowledge/collectors.json` for one project.
`knowledge collector add` writes the entry for you:

```
knowledge collector add --tool collect_loki_logs loki -- /home/you/.knowledge/bin/knowledge-collector-loki
```

Options come before the name and the command follows the `--`, mirroring
`claude mcp add`. Repeat `-e KEY=VALUE` for each environment name the entry must
declare, and add `--summarizable=true` / `--embeddable=true` to opt into LLM
summaries and vectors. The same entry written by hand:


```json
{
  "collectors": {
    "loki": {
      "type": "stdio",
      "command": "/home/you/.knowledge/bin/knowledge-collector-loki",
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
      "tool": "collect_loki_logs",
      "behavior": {
        "summarizable": true,
        "embeddable": true,
        "syncable": true
      },
      "env": {
        "LOKI_USERNAME": "<LOKI_USERNAME>",
        "LOKI_PASSWORD": "<LOKI_PASSWORD>",
        "LOKI_BEARER_TOKEN": "<LOKI_BEARER_TOKEN>",
        "LOKI_BEARER_TOKEN_FILE": "<LOKI_BEARER_TOKEN_FILE>",
        "LOKI_AUTH_HEADER": "<LOKI_AUTH_HEADER>",
        "LOKI_ORG_ID": "<LOKI_ORG_ID>",
        "LOKI_CA_CERT_PATH": "<LOKI_CA_CERT_PATH>",
        "LOKI_TLS_SKIP_VERIFY": "<LOKI_TLS_SKIP_VERIFY>",
        "LOKI_CLIENT_CERT_PATH": "<LOKI_CLIENT_CERT_PATH>",
        "LOKI_CLIENT_KEY_PATH": "<LOKI_CLIENT_KEY_PATH>",
        "LOKI_HTTP_PROXY_URL": "<LOKI_HTTP_PROXY_URL>",
        "LOKI_ENV_PROXY": "<LOKI_ENV_PROXY>",
        "LOKI_HTTP_COMPRESSION": "<LOKI_HTTP_COMPRESSION>",
        "LOKI_NO_CACHE": "<LOKI_NO_CACHE>",
        "LOKI_QUERY_TAGS": "<LOKI_QUERY_TAGS>",
        "LOKI_CLIENT_RETRIES": "<LOKI_CLIENT_RETRIES>",
        "LOKI_CLIENT_MIN_BACKOFF": "<LOKI_CLIENT_MIN_BACKOFF>",
        "LOKI_CLIENT_MAX_BACKOFF": "<LOKI_CLIENT_MAX_BACKOFF>",
        "HTTP_PROXY": "<HTTP_PROXY>",
        "http_proxy": "<http_proxy>",
        "HTTPS_PROXY": "<HTTPS_PROXY>",
        "https_proxy": "<https_proxy>",
        "NO_PROXY": "<NO_PROXY>",
        "no_proxy": "<no_proxy>",
        "SSL_CERT_FILE": "<SSL_CERT_FILE>",
        "SSL_CERT_DIR": "<SSL_CERT_DIR>"
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
between two templates; without them this collector resolves labels and confirms
nothing. `cloud-resource` is the one node type each of those inventory
collectors emits.

**`gcp` is deliberately absent.** The gcp collector emits its resource type AS
the node type — forty-nine `gcp:<service>:<kind>` values plus a `gcp-cidr-block`
sentinel — and a declaration selects a type only by naming it exactly, at one
node read per type per graph per collect. The family waits for an explicit
family-level selector rather than shipping a forty-nine-entry list or a key that
selects nothing.

There is no `graph` key and no `reason` key. The family is the map key, and the
loader refuses an unknown field by name — which fails the whole config file, not
just the entry that carried it.

The `env` block is the collector process's **whole environment**. Nothing is
inherited from the daemon, so a variable you do not list here does not reach the
collector however it is set on the machine running it. The block above lists
every variable this collector reads.

**Drop the lines you have no use for**, rather than leaving them empty. A
variable present in the block with an empty value is refused, naming it: an
empty value is something you chose, and treating it as unset would silently send
an unauthenticated request or ignore a header you meant to set. A variable that
is simply absent is unset, which is not an error.

**Every value above is a literal you replace, and none is a `${VAR}`
reference.** The config file does support that spelling, and for a credential it
is the RIGHT one: `"LOKI_PASSWORD": "${LOKI_PASSWORD}"` keeps the password out of
the file and the process serving the collect reads it from your own environment.
The install script writes exactly that for a credential name your installing
shell holds, and leaves the others out. This worked entry shows literals instead
because it lists every name at once, and a reference to a name you do not set
refuses this entry — naming the file, the entry, the key and the variable, and
leaving every other collector in the file collecting. So reference the credential
names you use, write the rest as literals, and **drop the lines you have no use
for**.

What is never right here is `${LOKI_PASSWORD:-}`: the default resolves to the
empty string in a serving process that does not hold the name, and this collector
refuses a present-and-empty name by name.

Two of them are not Loki's. `SSL_CERT_FILE` and `SSL_CERT_DIR` are read by Go's
own certificate verification on Linux and the BSDs. They are listed
unconditionally because a Loki endpoint is usually one you host yourself, often
behind a private certificate authority: without them, a collector on a host
whose certificate bundle is not at a compiled-in default cannot complete a TLS
handshake, and the error names neither the cause nor the fix.

## Configuring it

The **endpoint address is a parameter of the collect call**, not an environment
variable. `LOKI_ADDR` is deliberately not read: a collect names which Loki it is
reading from, so the address belongs beside the query rather than in the
process's environment.

Everything else comes from the environment, using the same variable names
[logcli](https://grafana.com/docs/loki/latest/query/logcli/) reads, so a Loki
you can already query from your shell needs no new configuration.

| Variable | What it does |
|---|---|
| `LOKI_USERNAME`, `LOKI_PASSWORD` | Basic authentication. Both or neither: one without the other is refused rather than sent as an empty password. |
| `LOKI_BEARER_TOKEN` | A bearer token, sent as `Bearer <token>`. |
| `LOKI_BEARER_TOKEN_FILE` | A file holding that token. Setting both spellings is refused. |
| `LOKI_AUTH_HEADER` | The header the bearer token goes into. Defaults to `Authorization`. |
| `LOKI_ORG_ID` | The tenant, sent as `X-Scope-OrgID`. |
| `LOKI_CA_CERT_PATH` | A certificate authority bundle to verify the endpoint against. |
| `LOKI_TLS_SKIP_VERIFY` | Skip certificate verification. A boolean. |
| `LOKI_CLIENT_CERT_PATH`, `LOKI_CLIENT_KEY_PATH` | A client certificate and its key. Both or neither. |
| `LOKI_HTTP_PROXY_URL` | Send every request through this proxy. |
| `LOKI_ENV_PROXY` | Honor the standard `HTTP_PROXY` family instead. A boolean. Those six names must be in the `env` block too, or there is nothing for it to read. |
| `LOKI_HTTP_COMPRESSION` | Ask Loki for compressed responses. A boolean. |
| `LOKI_NO_CACHE` | Send `Cache-Control: no-cache`. A boolean. |
| `LOKI_QUERY_TAGS` | Sent as `X-Query-Tags`, for Loki's own query attribution. |
| `LOKI_CLIENT_RETRIES` | How many times to retry a transport failure or a 5xx. Defaults to none. A 4xx is never retried. |
| `LOKI_CLIENT_MIN_BACKOFF`, `LOKI_CLIENT_MAX_BACKOFF` | The retry backoff, doubling from one to the other. Go durations, such as `500ms`. Default `500ms` and `5s`. |

A value that is not what its variable expects is an error naming the variable
and the value. Nothing is coerced: `LOKI_TLS_SKIP_VERIFY=yes` is refused rather
than read as false, and `LOKI_BEARER_TOKEN=` is refused rather than read as no
token at all.

## Calling it

| Parameter | Required | What it is |
|---|---|---|
| `address` | yes | The Loki endpoint, as an absolute `http` or `https` URL. |
| `start`, `end` | yes | The collect window, as RFC 3339 timestamps. |
| `selector` | no | A LogQL stream selector, braces included. When empty it is built from `source` and `field_filters`, and failing those it selects every stream carrying a `namespace` label. |
| `source` | no | A namespace, used only when `selector` is empty. |
| `field_filters` | no | Exact-match label filters, used only when `selector` is empty. The names `service`, `host`, `namespace`, `pod` and `container` map to Loki's own label names. |
| `text_filter` | no | Keep only entries whose message contains this text, case-insensitively. |
| `severity_min` | no | Keep only entries at or above this level: `TRACE`, `DEBUG`, `INFO`, `WARN`, `ERROR` or `CRITICAL`. |
| `raw_query` | no | LogQL appended to the built query verbatim. |

```json
{
  "id": "prod-checkout-logs",
  "params": {
    "address": "http://localhost:3100",
    "start": "2026-09-07T12:00:00Z",
    "end": "2026-09-07T13:00:00Z",
    "selector": "{app=\"checkout\"}",
    "severity_min": "WARN"
  }
}
```

The `id` names the graph the result lands in, and it is stable across windows:
collecting a second window under the same id updates that graph rather than
creating another. The collector never sees the graph's name or type.

## What the window bounds

The window is the only throttle. There is no entry cap and no size cap: the
collector pages backwards through the window until it is exhausted and returns
everything it read.

One case cannot be read whole, and the collector says so rather than pretending
otherwise. Loki's range query has no cursor, so paging works by moving the end
of the window below the oldest entry returned. If a full page of 5000 entries
all carry the same timestamp, moving below it would step over any further
entries at that instant, and nothing narrower can reach them. The walk stops
there and reports itself incomplete, naming the instant; collecting that instant
as its own window reads the rest. An incomplete walk also tells the daemon not
to treat anything it did not see as deleted.

Severity is read from each log line, not from Loki: a level in a JSON body wins
over one in the message text, which wins over one in the stream's labels. That
is why `severity_min` is applied after the entries arrive rather than pushed
into the query.

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
knowledge-collector-loki --describe-tool        # the tool name a config entry's `tool` field takes
knowledge-collector-loki --describe-env-table   # one `class name` row per declared variable
```

## Building it

```
go build ./cmd/collectors/loki
```

The module requires the collector framework and a compression library, and
nothing else of the knowledge tree. A test asserts that.
