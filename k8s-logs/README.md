# k8s-logs

A custom knowledge collector for Kubernetes pod and container logs.

It is an MCP provider: a binary the knowledge daemon spawns and speaks JSON-RPC
to over stdin and stdout. One collect call reads the pods you name, turns their
container logs into a log graph, and returns it. It reads the cluster through
the Kubernetes client library and never by running `kubectl`.

## What it produces

A log graph of four node types and three edge types.

| Node | What it is |
| --- | --- |
| `log-stream` | One container's log, identified by its labels. Two pods are two streams. |
| `log-template` | A message pattern, clustered from many lines with the variable parts replaced by wildcards. |
| `log-chunk` | A time-bounded, compressed block of the lines that matched one template on one stream. |
| `log-label` | A shared label value, so "every stream in this namespace" is a graph walk. |

| Edge | Direction |
| --- | --- |
| `BELONGS_TO` | chunk to stream |
| `CONTAINS` | template to chunk |
| `HAS_LABEL` | stream to label |

A chunk stores each line's timestamp and the tokens at its template's wildcard
positions, compressed. The template recovers the rest of the text, which is what
makes the graph much smaller than the log it came from.

## Installing it

Released builds live in the public **knowledge-contrib** repository: every
release there carries one archive per collector per platform, named
`knowledge-collector-k8s-logs-<os>-<arch>.tar.gz` (`.zip` on Windows) and holding a
single binary called `knowledge-collector-k8s-logs`, plus a `checksums.txt` covering
all of them. The install script in that repository is the way to get one: it
picks the archive for your platform, verifies it against `checksums.txt` and
installs nothing if the checksum is missing or does not match. Building from
source stays supported and is described below.

Install or build the binary, then add an entry to your `collectors.json`. The entry's NAME

The one command is:

```bash
curl -fsSL https://raw.githubusercontent.com/fulminate-io/knowledge-contrib/main/install.sh | sh -s -- k8s-logs
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
curl -fsSL https://raw.githubusercontent.com/fulminate-io/knowledge-contrib/main/uninstall.sh | sh -s -- k8s-logs
```

Or write the registration yourself.

Build the binary, then add an entry to your `collectors.json`. The entry's NAME
is the graph family the results land in.

Registration is a config file: `~/.knowledge/collectors.json` for this machine's
operator, or `<repository root>/.knowledge/collectors.json` for one project.
`knowledge collector add` writes the entry for you:

```
knowledge collector add --tool collect_k8s_logs k8s-logs -- /home/you/.knowledge/bin/knowledge-collector-k8s-logs
```

Options come before the name and the command follows the `--`, mirroring
`claude mcp add`. Repeat `-e KEY=VALUE` for each environment name the entry must
declare, and add `--summarizable=true` / `--embeddable=true` to opt into LLM
summaries and vectors. The same entry written by hand:


THIS BLOCK IS GENERATED, not hand-written: a test renders it from the same
function the collector ships and asserts this README contains it, so the two
cannot drift.

```json
{
  "collectors": {
    "k8s-logs": {
      "behavior": {
        "bm25_fields": [
          "symbol_name",
          "description",
          "summary"
        ],
        "embed_fields": [
          "symbol_name",
          "description",
          "summary"
        ],
        "embeddable": true,
        "summarizable": true,
        "summarize_fields": [
          "symbol_name",
          "description"
        ],
        "syncable": true
      },
      "command": "/home/you/.knowledge/bin/knowledge-collector-k8s-logs",
      "context": {
        "aws": {
          "node_types": [
            "cloud-resource"
          ],
          "node_fields": [
            "id",
            "type"
          ],
          "metadata_keys": [
            "namespace",
            "cluster_name",
            "resource_type",
            "region",
            "provider"
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
            "type"
          ],
          "metadata_keys": [
            "namespace",
            "cluster_name",
            "resource_type",
            "region",
            "provider"
          ],
          "edge_fields": [
            "from_id",
            "to_id"
          ]
        },
        "gcp": {},
        "k8s": {
          "node_types": [
            "cloud-resource"
          ],
          "node_fields": [
            "id",
            "type"
          ],
          "metadata_keys": [
            "namespace",
            "cluster_name",
            "resource_type",
            "region",
            "provider"
          ],
          "edge_fields": [
            "from_id",
            "to_id"
          ]
        }
      },
      "env": {
        "HOME": "/home/you",
        "KUBECONFIG": "/home/you/.kube/config",
        "PATH": "/usr/local/bin:/usr/bin:/bin"
      },
      "node_types": {
        "log-chunk": {
          "bm25_fields": [
            "symbol_name"
          ],
          "embeddable": false
        }
      },
      "tool": "collect_k8s_logs",
      "type": "stdio"
    }
  }
}
```

### The `context` block, and what each entry is for

The `context` key is keyed by graph family: the name a provider collector is
registered under. The client fills exactly what each family declares and nothing
else, and it refuses a family this daemon has no collector registered for,
naming it and listing the ones it can supply — so delete the entries for
providers you do not run.

`aws`, `azure` and `k8s` ask for the `cloud-resource` node type, which is the one
node type each of those collectors emits; the resource kind rides in the
`resource_type` metadata key. The declared metadata keys are what a log stream's
labels are matched against, and the declared `from_id` / `to_id` edge fields are
what CONFIRM a temporal correlation between two templates. A declaration without
the edges yields correlations this collector cannot stand behind.

`gcp` declares the family with **no node types**, and that is a disclosure rather
than an omission: the entry receives the gcp graph names and no nodes and no
edges, so no gcp resource is resolved and no gcp correlation is confirmed. GCP
emits its resource type AS the node type — forty-nine `gcp:<service>:<kind>`
values plus the `gcp-cidr-block` sentinel — and a declaration selects a type only
by naming it exactly, at one node read per type per graph per collect.
Enumerating forty-nine of them is the wrong artifact to ship; an explicit
family-level selector is what restores this arm.

There is no `graph` key and no `reason` key. The family is the map key, and the
loader refuses an unknown field by name — which fails the whole config file, not
just the entry that carried it.

Then collect, naming the graph instance and the namespaces to read:

```jsonc
collect(type: "k8s-logs", id: "prod-window", params: {
  "context": "my-cluster",
  "namespaces": ["dev"],
  "label_selector": "app=api",
  "since": "2026-09-07T10:00:00Z",
  "until": "2026-09-07T11:00:00Z"
})
```

### The environment block is the whole environment

A `type: stdio` collector receives exactly the variables its `env` block names.
The daemon copies nothing from its own environment, so a variable the block does
not list is ABSENT from this process however the daemon was started.

The names this collector needs:

| Target | Names |
| --- | --- |
| Linux, macOS | `HOME`, `KUBECONFIG`, `PATH` |
| Windows | those, plus `HOMEDRIVE`, `HOMEPATH`, `USERPROFILE`, `SYSTEMROOT` |
| Running inside a cluster | add `KUBERNETES_SERVICE_HOST`, `KUBERNETES_SERVICE_PORT` |

`PATH` is load-bearing even when your kubeconfig names its auth plugin by
absolute path: the plugin itself runs another binary, and that spawn resolves
through `PATH`.

**`HOME` must be real or absent, never an empty placeholder.** This inverts the
usual intuition. Leaving `HOME` unset works — a cloud auth plugin falls back to
the password database. Setting it to an empty directory FAILS, because the
plugin then looks for credentials in a home that has none.

Nothing else is listed, and each omission is a choice with a cost:

- **Proxy variables** (`HTTP_PROXY` and its siblings) are left off so a collect
  and your own `kubectl` cannot silently disagree about what is reachable. If
  you are behind a corporate proxy, list them in the entry; without them you get
  a connection error rather than an explanation.
- **Trust roots** (`SSL_CERT_FILE`, `SSL_CERT_DIR`) are left off on the same
  footing. On a host whose CA bundle is not at a compiled-in default, list them.
- **`KUBERNETES_MASTER` and `POD_NAMESPACE`** are left off deliberately: each
  would silently override a parameter this tool owns.
- **Client feature gates** are left off: a gate flipped from outside the tool's
  parameters changes behavior with nothing in the result saying so.

## Parameters

Every parameter is optional except the namespaces.

| Parameter | Meaning |
| --- | --- |
| `context` | The kubecontext to read. Empty uses the kubeconfig's current context. A context the kubeconfig does not hold is an error naming it, never a fall-through. |
| `namespaces` | The namespaces to read. **Required.** There is no cluster-wide default: at the API an empty namespace means every namespace, so this collector makes you name them. |
| `label_selector` | Narrows the pod list, in the API's own selector syntax. |
| `containers` | Container names to read. Empty reads every container of each pod, **including every init container**. |
| `since` | RFC3339 start of the range, applied server-side. |
| `until` | RFC3339 end of the range, applied client-side: the Kubernetes API has no end bound. |
| `tail_lines` | Read only the last N lines of each container. Unset reads the whole retained log. |
| `limit_bytes` | Stop each container's read after N bytes. Unset reads the whole retained log. |
| `stream` | `all` (the default), `stdout` or `stderr`. The API accepts `tail_lines` only with the combined stream. |
| `chunk_window_seconds` | Chunk bucket width. Zero uses the default of five minutes. |

### Init containers are log sources

A pod's log sources are its regular containers **and** its init containers. On a
service mesh those are long-running sidecars carrying most of the pod's output,
so a collect that enumerated only the regular containers would silently omit
them. This one does not.

### Severity comes from the message, not from the stream

Many container runtimes surface everything a container writes to stderr as
ERROR, whatever the application meant. So the level on each entry is read from
the line's own text — a `level=` field, a bracketed level, a leading level word
— and falls back to INFO. Use the `stream` parameter to choose which stream you
read; the level still comes from the body.

## Completeness and deletion

Every result carries an assertion about whether the walk enumerated everything
it was asked for. A complete collect lets the server treat rows it did not carry
as gone, so this collector asserts completeness conservatively: TRUE only when
every container it named was read to the end of the requested window with no
bound engaging, and FALSE the moment a bound cut a stream short, a pod could not
be read, or a line arrived without a parseable timestamp.

A namespace that cannot be listed does not fail the collect. It is reported as a
reason the walk is incomplete, so the result carries what WAS read and the
server declines to treat the rest as gone.

## Stability across collects

A stream's identity is a hash of its labels, so a re-collect of an unchanged
source reproduces the same node ids and the graph carries forward rather than
being rebuilt. That is why the label set excludes everything that moves under
the collector: restart counts, timestamps, pod UIDs and the stream a line
arrived on.

The chunk window is aligned to the UTC epoch rather than to the first entry, so
two collects covering overlapping ranges agree about where the bucket
boundaries are.

## Cloud resources

When the collect input carries a foreign-graph context block naming cloud
resources, this collector matches its streams' namespaces against them and emits
proxy nodes with `EMITTED_BY` edges from the matching labels, plus
`CORRELATES_WITH` edges between error templates in two services whose resources
the supplied slice says depend on one another. A temporal overlap alone is a
candidate and never an edge.

With no cloud context supplied it emits none of the three, which is the same
thing the built-in log pipeline does with no cloud graph attached.

**The block is declared, never queried.** Your config entry says which cloud
graphs and which node fields this collector needs; the client reads them out of
your own graphs and sends them with the call. An entry that declares nothing
receives nothing, and the collect then emits none of the three. That zero is
correct rather than degraded: nothing was dropped and no cloud read failed,
there is simply nothing to resolve against.

## What it declares about itself

This collector serves the contract's required `describe` tool: one document
naming its suggested behavior and field lists, its five node types and its five edge types, the environment
variables it reads with what an installer must do with each, the cloud-provider context it correlates against, and
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
knowledge-collector-k8s-logs --describe-tool        # the tool name a config entry's `tool` field takes
knowledge-collector-k8s-logs --describe-env-table   # one `class name` row per declared variable
```

## Building and testing

```bash
cd cmd/collectors/k8s-logs
go build ./...
go test ./...
```

The tests need no cluster, no network and no daemon: pod listing and log
streaming run against an in-test HTTP server serving the two real endpoints
through a real client, and the MCP round trip re-execs the test binary as a
stdio provider.
