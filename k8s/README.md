# Kubernetes collector

A Kubernetes collector for the knowledge graph, served as an MCP provider over
stdio. It enumerates a cluster's objects and the relationships between them and
returns them as one collect result, into a graph family of its own.

It reads a cluster and never writes to one. Every API call it makes is a list.

## What it collects

**32 fixed kinds, plus every custom resource the cluster serves.** Workloads,
pods and nodes; services, endpoint slices, ingresses and network policies;
config maps, secrets, service accounts and the four RBAC kinds; claims, volumes
and storage classes; autoscalers and disruption budgets; the four Gateway API
kinds and the two AdminNetworkPolicy kinds; and one node per instance of every
custom resource definition the cluster serves.

**33 relationship types.** Eighteen are stated by an object about itself: what a
workload mounts, what it runs as, what an ingress routes to, what a binding
binds. Nine are derived from the enumeration as a whole: namespace membership,
label-selector matching, and network-policy reachability between pods. Six point
at a resource in another graph — a load balancer, a virtual machine, a cloud
identity, a disk, an external service, the managed cluster itself — and carry a
proxy node for it so both ends of the edge exist.

**Secrets are enumerated through an allowlist.** Only an explicit set of fields
reaches the graph: identity, the type, labels, two well-known annotations that
name a service account, and the key NAMES and count. Everything else is dropped,
including annotations and managed fields.

The allowlist is not a preference over a strip list. A strip list has to
enumerate every path a value can take, and it was defeated by the most ordinary
secret in any cluster: one written with `kubectl apply` keeps its whole
manifest, data block included, in
`kubectl.kubernetes.io/last-applied-configuration`, which reached both the
object body and the node's metadata. The graph a collect lands in is indexed and
replicated, so a value reaching it is a credential in a searchable store.

### Node identity

A node's id is `namespace/Kind/name`, or `Kind/name` for a cluster-scoped
object. Labels are recorded under `label/`, annotations under `annotation/`.

## Installing it

Released builds live in the public **knowledge-contrib** repository: every
release there carries one archive per collector per platform, named
`knowledge-collector-k8s-<os>-<arch>.tar.gz` (`.zip` on Windows) and holding a
single binary called `knowledge-collector-k8s`, plus a `checksums.txt` covering
all of them. The install script in that repository is the way to get one: it
picks the archive for your platform, verifies it against `checksums.txt` and
installs nothing if the checksum is missing or does not match. Building from
source stays supported and is described below.

The one command is:

```bash
curl -fsSL https://raw.githubusercontent.com/fulminate-io/knowledge-contrib/main/install.sh | sh -s -- k8s
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
curl -fsSL https://raw.githubusercontent.com/fulminate-io/knowledge-contrib/main/uninstall.sh | sh -s -- k8s
```

Or write the registration yourself.

Install or build the binary, then add an entry to your collector config file.

Registration is a config file: `~/.knowledge/collectors.json` for this machine's
operator, or `<repository root>/.knowledge/collectors.json` for one project.
`knowledge collector add` writes the entry for you:

```
knowledge collector add --tool collect k8s -- /home/you/.knowledge/bin/knowledge-collector-k8s
```

Options come before the name and the command follows the `--`, mirroring
`claude mcp add`. Repeat `-e KEY=VALUE` for each environment name the entry must
declare, and add `--summarizable=true` / `--embeddable=true` to opt into LLM
summaries and vectors.

The entry below is what `ExampleEntry` generates; the `env` block is the
collector's own declared list, so it does not drift from what the collector
actually reads.

```json
{
  "collectors": {
    "k8s": {
      "type": "stdio",
      "command": "/home/you/.knowledge/bin/knowledge-collector-k8s",
      "tool": "collect",
      "env": {
        "CLOUDSDK_CONFIG": "${CLOUDSDK_CONFIG:-}",
        "DISABLE_HTTP2": "${DISABLE_HTTP2:-}",
        "HOME": "${HOME:-}",
        "HTTP2_PING_TIMEOUT_SECONDS": "${HTTP2_PING_TIMEOUT_SECONDS:-}",
        "HTTP2_READ_IDLE_TIMEOUT_SECONDS": "${HTTP2_READ_IDLE_TIMEOUT_SECONDS:-}",
        "HTTPS_PROXY": "${HTTPS_PROXY:-}",
        "HTTP_PROXY": "${HTTP_PROXY:-}",
        "KUBECONFIG": "${KUBECONFIG:-}",
        "KUBERNETES_MASTER": "${KUBERNETES_MASTER:-}",
        "KUBERNETES_SERVICE_HOST": "${KUBERNETES_SERVICE_HOST:-}",
        "KUBERNETES_SERVICE_PORT": "${KUBERNETES_SERVICE_PORT:-}",
        "NO_PROXY": "${NO_PROXY:-}",
        "PATH": "${PATH:-}",
        "POD_NAMESPACE": "${POD_NAMESPACE:-}",
        "SSL_CERT_DIR": "${SSL_CERT_DIR:-}",
        "SSL_CERT_FILE": "${SSL_CERT_FILE:-}",
        "http_proxy": "${http_proxy:-}",
        "https_proxy": "${https_proxy:-}",
        "no_proxy": "${no_proxy:-}"
      },
      "behavior": {
        "summarizable": true,
        "embeddable": true,
        "syncable": true,
        "bm25_fields": [
          "symbol_name",
          "description",
          "content"
        ],
        "summarize_fields": [
          "content"
        ],
        "embed_fields": [
          "summary",
          "description"
        ]
      },
      "context": {
        "azure": {
          "node_types": [
            "cloud-resource"
          ],
          "node_fields": [
            "id"
          ],
          "metadata_keys": [
            "client_id"
          ]
        },
        "code": {
          "node_types": [
            "file"
          ],
          "node_fields": [
            "id",
            "file_path",
            "content"
          ],
          "path_basenames": [
            "Chart.yaml",
            "Chart.yml"
          ]
        }
      }
    }
  }
}
```

The real entry lists every name in the section below. A graph registered without
the `behavior` opt-in is collected and walkable but never enters the text index,
and a search over it reads zero with nothing that looks like a failure — so all
three flags are set explicitly.

### The `context` block, and what each entry is for

The `context` key is keyed by graph family and declares what this collector needs
from other graphs. The client fills exactly that and nothing else, and it refuses
a family this daemon has no collector registered for, naming it and listing the
ones it can supply — so delete the `azure` entry if you do not run the azure
collector.

`code` is the **DEPLOYS** shape: the chart name is parsed out of a `Chart.yaml`
file's body and the edge is emitted FROM that file node's id, so the entry asks
for the id, the path and the content, narrowed to the two chart basenames.

`azure` is the **WORKLOAD_IDENTITY** shape: an `azure.workload.identity/client-id`
annotation is a bare UUID that names nothing on its own, so the entry asks for
azure resources' ids and their `client_id` metadata. It names the `cloud-resource`
node type because that is the one node type the azure collector emits — the Azure
resource kind rides in the `resource_type` metadata key rather than in the node
type. The IRSA and GCP identity shapes compose their targets from the service
account's own metadata and declare nothing.

There is no `graph` key and no `reason` key. The family is the map key, and the
loader refuses an unknown field by name — which fails the whole config file, not
just the entry that carried it.

## Credentials

The collector is handed no credential. It resolves one the standard way, from
the environment its config entry declares:

1. **In-cluster**, when `KUBERNETES_SERVICE_HOST` and `KUBERNETES_SERVICE_PORT`
   are set. The service-account token and CA bundle are read from their mount
   paths. If the environment says in-cluster and the files are not readable, the
   collect fails naming the file. It does not fall back to a kubeconfig: a pod
   with a broken service account authenticating as whoever's kubeconfig is on
   the image is a worse outcome than a refusal.
2. **A kubeconfig**, from `KUBECONFIG` if set, otherwise `$HOME/.kube/config`.

With neither available, the collect fails naming every input it looked for. It
never invents a home directory.

### Selecting a cluster

The collect takes one optional parameter, `context`, naming a kubeconfig
context. Omitted, the kubeconfig's currently selected context is used. A named
context the kubeconfig does not carry is an error listing the contexts it does
carry — never a silent fall back to the current one, which would collect the
wrong cluster under the id you asked for.

### The declared environment

A stdio collector's environment is exactly what its config entry declares: the
daemon copies nothing from its own and adds nothing. So a name missing from the
entry is a variable this process never receives, and the absence is silent.

| Name | Why |
|---|---|
| `KUBECONFIG`, `HOME` | Where the kubeconfig is found |
| `PATH` | A credential plugin runs another program to mint a token, and finds it here |
| `CLOUDSDK_CONFIG` | Substitutes for the cloud SDK's configuration directory |
| `KUBERNETES_SERVICE_HOST`, `KUBERNETES_SERVICE_PORT` | The in-cluster arm |
| `KUBERNETES_MASTER`, `POD_NAMESPACE` | Read by the kubeconfig loader itself |
| `HTTP_PROXY`, `HTTPS_PROXY`, `NO_PROXY` and their lower-case spellings | Consulted by the transport on every call |
| `DISABLE_HTTP2`, `HTTP2_READ_IDLE_TIMEOUT_SECONDS`, `HTTP2_PING_TIMEOUT_SECONDS` | Transport tuning, read on the same path |
| `SSL_CERT_FILE`, `SSL_CERT_DIR` | The trust roots, on Linux and the other unix targets |
| `HOMEDRIVE`, `HOMEPATH`, `USERPROFILE` | The home-directory fallbacks, on Windows |

The upper- and lower-case proxy spellings are two different lookups tried in
order, so declaring one of a pair changes behavior rather than halving the cost.
The trust-root pair has no reader compiled on macOS or Windows and is not listed
there; the home fallbacks are listed on Windows only.

Two notes measured against a real managed cluster, because they are the reverse
of the intuitive ones:

- **`PATH` is load-bearing** even when the kubeconfig names its credential
  plugin by absolute path, because the plugin runs another program.
- **An absent `HOME` works; a `HOME` pointing at an empty directory does not.**
  If you are not passing a real home directory, omit the variable rather than
  setting it to a placeholder.

Set the values in your own environment; the `${VAR:-}` references in the entry
carry them in, so no credential is written into a config file.

## Completeness

Every collect asserts whether it enumerated the whole cluster, and that
assertion decides whether the server may treat what the collect did not carry as
deleted. This collector asserts an incomplete walk whenever it did not see
everything, and names what it missed:

- An API group the cluster does not serve.
- A listing the API server truncated.
- A custom resource with more instances than the per-resource bound, which the
  collector pages up to and then stops at, saying how many it reached.

A failure it cannot survive — a permission refusal, an unreachable API server, a
transport failure — fails the collect outright rather than returning a partial
result. A partial graph asserting a complete walk would hand the server a
deletion basis built out of an outage.

## Cross-graph edges

Two relationship types name an endpoint in another graph: a Helm chart to what
it deploys, and a service account to the cloud identity it assumes.

**All four are emitted**, each carrying the graph family its far endpoint lives
in. The client resolves that endpoint against the family's loaded graphs and
links the edge into the linkage graph.

**Which field a shape uses says which end is foreign.** Three of them put the
Kubernetes node on the near side and name the family in `target_graph`: a
service account to its cloud identity on each of the three providers. The Helm
relationship runs the other way — from the chart file to the Kubernetes object —
so it names the family in `source_graph` and leaves `target_graph` empty.
Setting both on one edge is refused by the collect, because one resolution
reaches one foreign family.

**A workload to the source repository its image was built from is not emitted**,
because that edge's far endpoint is a code graph's name and a code graph holds
no node denoting a repository for it to resolve against.

The Helm shape was computed and held while the contract had only the
target-graph field, since reversing the edge would have asserted a relationship
the built-in linker does not and emitting it with no family would have landed an
in-graph edge to a chart id nothing here resolves. The mirror field is the third
option and this collector now takes it. A test reads the contract edge's fields
by reflection, so dropping either one goes red rather than silently turning
these edges into dangling in-graph ones.

## What it declares about itself

This collector serves the contract's required `describe` tool: one document
naming its suggested behavior and field lists, its two node types and its thirty-five edge types, the environment
variables it reads with what an installer must do with each, the foreign-graph context it needs, and
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
knowledge-collector-k8s --describe-tool        # the tool name a config entry's `tool` field takes
knowledge-collector-k8s --describe-env-table   # one `class name` row per declared variable
```

## Development

```
go test ./...
```

The tests use fake clientsets and no cluster. The one place a fake is not enough
is paging: the fake clientsets ignore the page limit and the continuation token
entirely, so the truncation is built with a reactor instead. `cap_test.go`
measures that blind spot in the same run, so the reactor is a demonstrated
necessity rather than an assertion.

Every declared relationship type carries an emit-and-suppress pair in
`edge_census_test.go`, and a type with no pair fails the census. That exists
because one family shipped with only its suppress half, so disabling its whole
branch left the suite green while the type stayed in the declared vocabulary.
