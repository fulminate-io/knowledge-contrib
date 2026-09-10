# gcp collector

A knowledge collector for Google Cloud. It serves one tool over MCP on stdio: a
daemon spawns it, calls that tool with a collect id and a project, and writes
what it returns into a graph of its own.

It reads. It never writes to your cloud, and it asks for a read-only credential
scope, so it cannot.

## What it produces

One node per resource and one edge per relationship, for 49 resource kinds
across compute, networking, load balancing, containers, serverless, storage,
databases, messaging, identity, keys, DNS, observability and analytics. Node ids
are the provider's own: a compute self-link where the API has one, the relative
resource name everywhere else, a scheme-qualified name for a storage bucket.

Three kinds of node are synthetic, because the provider has no resource to point
at: a firewall CIDR block, an external routing peer, and the upstream a mirror
repository proxies. Each says in its metadata that it was referenced rather than
read, so you can tell it from a resource the walk failed to describe.

Six relationships are DERIVED rather than enumerated — nothing in the API
returns them, and they are computed over the walk's own contents before it
returns:

| Derived | What it says |
|---|---|
| instance reachability | which instances a firewall rule actually lets talk, and to which address ranges |
| shared network linkage | a subnet whose parent network lives in another project |
| image lineage | which repository a running service's image comes from |
| cross-project trust | a service account in another project holding an impersonation role here |
| record targeting | which resource answers on the address a DNS record publishes |
| group binding resolution | a policy binding naming a group by email, resolved onto the group's own node |

The last one also REMOVES the unresolved placeholder, so the graph holds no
permanently dangling group reference.

## Building it

```
cd cmd/collectors/gcp
go build -o gcp .
```

The binary has no flags and no configuration of its own. Everything it needs
arrives on the tool call or in its environment.

## Installing it

Released builds live in the public **knowledge-contrib** repository: every
release there carries one archive per collector per platform, named
`knowledge-collector-gcp-<os>-<arch>.tar.gz` (`.zip` on Windows) and holding a
single binary called `knowledge-collector-gcp`, plus a `checksums.txt` covering
all of them. The install script in that repository is the way to get one: it
picks the archive for your platform, verifies it against `checksums.txt` and
installs nothing if the checksum is missing or does not match. Building from
source stays supported and is described below.

The one command is:

```bash
curl -fsSL https://raw.githubusercontent.com/fulminate-io/knowledge-contrib/main/install.sh | sh -s -- gcp
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
curl -fsSL https://raw.githubusercontent.com/fulminate-io/knowledge-contrib/main/uninstall.sh | sh -s -- gcp
```

Or write the registration yourself.
A collector is installed by writing an entry into a JSON config file. There is no
registration command and no server-side record: the entry IS the registration,
and removing it unregisters the collector at the next lookup.

The file is `collectors.json`, in either of two scopes:

- `~/.knowledge/collectors.json` for this machine's operator;
- `<repository root>/.knowledge/collectors.json` for one project.

Where a name is in both, the project entry wins, and it wins WHOLE: no field is
merged across scopes.

The entry's key is the graph family the results land in, so this one is `gcp`.
`knowledge collector add` writes it for you, mirroring `claude mcp add` — options
before the name, the command after the `--`:

```
knowledge collector add --tool collect gcp -- <HOME>/.knowledge/bin/knowledge-collector-gcp
```

Repeat `-e KEY=VALUE` for each environment name the entry must declare, and add
`--summarizable=true` / `--embeddable=true` to opt into LLM summaries and
vectors. The same entry written by hand:

```json
{
  "collectors": {
    "gcp": {
      "type": "stdio",
      "command": "<HOME>/.knowledge/bin/knowledge-collector-gcp",
      "env": {
        "EXPERIMENTAL_GOOGLE_API_USE_S2A": "<EXPERIMENTAL_GOOGLE_API_USE_S2A>",
        "GCE_METADATA_HOST": "<GCE_METADATA_HOST>",
        "GOOGLE_API_CERTIFICATE_CONFIG": "<GOOGLE_API_CERTIFICATE_CONFIG>",
        "GOOGLE_API_GO_EXPERIMENTAL_DISABLE_NEW_AUTH_LIB": "<GOOGLE_API_GO_EXPERIMENTAL_DISABLE_NEW_AUTH_LIB>",
        "GOOGLE_API_GO_EXPERIMENTAL_ENABLE_NEW_AUTH_LIB": "<GOOGLE_API_GO_EXPERIMENTAL_ENABLE_NEW_AUTH_LIB>",
        "GOOGLE_API_USE_CLIENT_CERTIFICATE": "<GOOGLE_API_USE_CLIENT_CERTIFICATE>",
        "GOOGLE_API_USE_MTLS": "<GOOGLE_API_USE_MTLS>",
        "GOOGLE_API_USE_MTLS_ENDPOINT": "<GOOGLE_API_USE_MTLS_ENDPOINT>",
        "GOOGLE_APPLICATION_CREDENTIALS": "<GOOGLE_APPLICATION_CREDENTIALS>",
        "GOOGLE_AUTH_TRUST_BOUNDARY_ENABLED": "<GOOGLE_AUTH_TRUST_BOUNDARY_ENABLED>",
        "GOOGLE_CLOUD_PROJECT": "<GOOGLE_CLOUD_PROJECT>",
        "GOOGLE_CLOUD_QUOTA_PROJECT": "<GOOGLE_CLOUD_QUOTA_PROJECT>",
        "GOOGLE_CLOUD_UNIVERSE_DOMAIN": "<GOOGLE_CLOUD_UNIVERSE_DOMAIN>",
        "HOME": "<HOME>",
        "HTTPS_PROXY": "<HTTPS_PROXY>",
        "HTTP_PROXY": "<HTTP_PROXY>",
        "NO_PROXY": "<NO_PROXY>",
        "S2A_TIMEOUT": "<S2A_TIMEOUT>",
        "SSL_CERT_DIR": "<SSL_CERT_DIR>",
        "SSL_CERT_FILE": "<SSL_CERT_FILE>",
        "http_proxy": "<http_proxy>",
        "https_proxy": "<https_proxy>",
        "no_proxy": "<no_proxy>"
      },
      "tool": "collect",
      "behavior": {
        "syncable": true,
        "summarizable": true,
        "embeddable": true,
        "embed_fields": [
          "summary",
          "symbol_name"
        ],
        "summarize_fields": [
          "content",
          "symbol_name",
          "summary"
        ],
        "bm25_fields": [
          "symbol_name",
          "summary",
          "content"
        ]
      }
    }
  }
}
```

Replace `command` with where you put the binary, and each environment value with
your own. The loader is strict: a key it does not know, a missing `type` or a
missing `tool` is a refusal naming the file, the entry and the field, not an
entry it quietly skips.

## The behavior block is what makes the graph searchable

The three booleans are the pipeline's switches, and an absent one already
defaults to true. They are written out anyway, because this is the block you edit
to turn one off and a block that is not there is one you cannot find.

The three FIELD LISTS have no default at all, and that is why they matter. A
graph whose lists are empty is collected, readable by id and walkable, and never
enters the text index: searching it reads zero and keeps reading zero. Each list
names node fields this collector actually populates.

| List | Fields | Why |
|---|---|---|
| `embed_fields` | summary, symbol_name | the sentence and the name; the content is a JSON document and embedding one spends the vector on punctuation |
| `summarize_fields` | content, symbol_name, summary | the resource's actual configuration is what there is to summarize |
| `bm25_fields` | symbol_name, summary, content | a keyword search for an address, a label value or a machine type finds it in the content and nowhere else |

There is no `node_types` block. This collector's node type IS the resource type,
so there are 49 of them and they all want the same treatment; a per-type override
would be 49 copies of the answer above.

## The environment block is not optional

A stdio collector receives EXACTLY the environment its entry declares. The
daemon copies nothing from its own environment and adds nothing, so a variable
you do not list is a variable this collector does not have.

That matters more than it sounds, because the consequences of omitting one are
not uniform:

- Most are switches whose absence means "take the default", which is usually
  what you want.
- One fails LOUDLY: on a non-default universe the client falls back to the
  public endpoint while your credential carries the real one, and the mismatch
  is reported naming both.
- One fails SILENTLY in the direction you will not expect: the switch that
  disables the newer credential implementation is read FIRST and overrides the
  option every client sets, so omitting it makes your override do nothing.
- Behind a proxy, or on a host whose certificate bundle is not at a compiled-in
  default, omitting the proxy or trust-root names gives you a collector that
  cannot reach the provider a browser on the same host can, and the failure is
  a transport error naming neither cause.

The block above is for a Linux or macOS host. On Windows, replace `HOME` with
`APPDATA`. Nothing else changes.

Four names you may expect and will not find there, a config directory and three
alternative project variables, are read by nothing this collector depends on.
One more is left out deliberately: a variable carrying an access token, because
a credential belongs in the credential chain and not in a configuration file. If
you rely on that token, its acquisition will fail inside this collector and work
outside it.

## Credentials

Application Default Credentials, and nothing else. This collector takes no
credential from its caller, has no flag for one, and requests only a read-only
scope.

The chain looks, in order, for the file named by
`GOOGLE_APPLICATION_CREDENTIALS`, then the logged-in account's well-known file
under the home directory, then the instance metadata server. Every name in that
sentence must be in the block above for the child process to see it.

If it finds nothing, the collect fails with an error naming the chain and its
variables. It does not fall back to an unauthenticated call.

## Collecting

```json
{"id": "my-project-graph", "params": {"project": "my-project"}}
```

The `id` names the graph the results land in. The `project` names what to
enumerate, and it is REQUIRED: this collector reads no environment variable to
resolve a project, so a call without one is refused rather than walked against
whatever the host happened to be configured for.

## Anything this collector did not see is reported as not seen

Three outcomes make a collect INCOMPLETE, and the reason names which
enumerations and which kind.

| Outcome | What happened | What you do |
|---|---|---|
| refused | the provider answered "not with this credential" | enable the API or grant the role |
| partially read | the provider answered and said part is missing: an unreachable zone, a failed location | usually nothing; it is the provider's and it passes |
| failed | the call did not complete | investigate the service |

None of the three fails the collect. Every other enumeration still runs and
everything they found is still returned, including whatever page a refused or
partially read enumeration had already collected.

WHY THE INCOMPLETE MARK MATTERS MORE THAN IT LOOKS. A COMPLETE collect lets the
receiving server treat everything the collect did not carry as gone. A collector
that reported an unreadable zone as an ordinary empty result would therefore, on
the next collect, delete every resource in that zone and report success. That is
the one failure this collector is built hardest against, and it is why a region
that was merely unreachable for a minute costs you a mark on the collect rather
than your graph.

If EVERY enumeration FAILS, the collect fails outright: a walk that learned
nothing is not a partial answer. If every enumeration is refused or partially
read, the collect succeeds and is marked incomplete, because the provider
answered every time.

## Two collects of an unchanged project produce the same result

The output is sorted by node id and by edge, so a second collect of a project
nothing changed is identical to the first, byte for byte. A diff between two
collects shows what moved in your cloud and nothing else.

## What it declares about itself

This collector serves the contract's required `describe` tool: one document
naming its suggested behavior and field lists, every resource type it emits and its edge types, the environment
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
knowledge-collector-gcp --describe-tool        # the tool name a config entry's `tool` field takes
knowledge-collector-gcp --describe-env-table   # one `class name` row per declared variable
```

## Running its tests

```
cd cmd/collectors/gcp
go test ./...
```

No credential and no network. Every converter runs against a recorded API
response, and the suite asserts that the whole collector reaches every resource
kind and every relationship it declares.
