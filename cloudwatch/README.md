# CloudWatch Logs collector

A knowledge collector for AWS CloudWatch Logs. It is an MCP provider: the
knowledge daemon spawns it, calls one tool, and writes what it returns into a
graph of its own.

It reads log groups over a time window and returns a graph of the logs rather
than the log lines. Messages that share a shape become one template; label sets
become streams; the entries of one template in one stream in one five-minute
window become a chunk. Low-cardinality labels become shared nodes so you can
ask which streams carry a label without walking every stream.

## What it produces

| Node | What it is |
| --- | --- |
| `log-template` | A message skeleton, with the variable positions replaced by `<*>`. Carries the pattern, the maximum severity over its entries, a count and a time range. |
| `log-stream` | One distinct label set. Carries every label, plus a fingerprint over the low-cardinality ones. |
| `log-chunk` | The entries of one template, in one stream, in one five-minute window. Its content is one line per entry: the timestamp, a tab, the message. |
| `log-label` | One low-cardinality label pair, shared across every stream that carries it. |
| `proxy` | A reference to a resource in a cloud graph. Emitted only when the collect supplies cloud resources to resolve labels against. |

| Edge | From | To |
| --- | --- | --- |
| `HAS_LABEL` | a stream | a label |
| `BELONGS_TO` | a chunk | its stream |
| `CONTAINS` | a template | its chunks |
| `EMITTED_BY` | a label | a cloud proxy |
| `CORRELATES_WITH` | a template | a template |

Every identifier is derived from the events themselves, so collecting the same
log group twice produces the same identifiers and the second collect carries the
unchanged nodes forward instead of replacing the graph. The five-minute windows
are aligned to the epoch rather than to the requested range, which is what keeps
that true when you widen or narrow the window between collects.

## Installing it

Released builds live in the public **knowledge-contrib** repository: every
release there carries one archive per collector per platform, named
`knowledge-collector-cloudwatch-<os>-<arch>.tar.gz` (`.zip` on Windows) and holding a
single binary called `knowledge-collector-cloudwatch`, plus a `checksums.txt` covering
all of them. The install script in that repository is the way to get one: it
picks the archive for your platform, verifies it against `checksums.txt` and
installs nothing if the checksum is missing or does not match. Building from
source stays supported and is described below.

Install or build the binary and add an entry to your collector configuration
file. The entry name is the graph family the results land in.

The one command is:

```bash
curl -fsSL https://raw.githubusercontent.com/fulminate-io/knowledge-contrib/main/install.sh | sh -s -- cloudwatch
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
curl -fsSL https://raw.githubusercontent.com/fulminate-io/knowledge-contrib/main/uninstall.sh | sh -s -- cloudwatch
```

Or write the registration yourself.

Registration is a config file: `~/.knowledge/collectors.json` for this machine's
operator, or `<repository root>/.knowledge/collectors.json` for one project.
`knowledge collector add` writes the entry for you:

```
knowledge collector add --tool collect cloudwatch -- /home/you/.knowledge/bin/knowledge-collector-cloudwatch
```

Options come before the name and the command follows the `--`, mirroring
`claude mcp add`. Repeat `-e KEY=VALUE` for each environment name the entry must
declare, and add `--summarizable=true` / `--embeddable=true` to opt into LLM
summaries and vectors. The same entry written by hand:

```
go build -o knowledge-collector-cloudwatch .
```

```jsonc
{
  "collectors": {
    "cloudwatch": {
      "type": "stdio",
      "command": "/home/you/.knowledge/bin/knowledge-collector-cloudwatch",
      "env": {
        "AWS_ACCESS_KEY": "${AWS_ACCESS_KEY:-}",
        "AWS_ACCESS_KEY_ID": "${AWS_ACCESS_KEY_ID:-}",
        "AWS_ACCOUNT_ID": "${AWS_ACCOUNT_ID:-}",
        "AWS_ACCOUNT_ID_ENDPOINT_MODE": "${AWS_ACCOUNT_ID_ENDPOINT_MODE:-}",
        "AWS_AUTH_SCHEME_PREFERENCE": "${AWS_AUTH_SCHEME_PREFERENCE:-}",
        "AWS_CA_BUNDLE": "${AWS_CA_BUNDLE:-}",
        "AWS_CONFIG_FILE": "${AWS_CONFIG_FILE:-}",
        "AWS_CONTAINER_AUTHORIZATION_TOKEN": "${AWS_CONTAINER_AUTHORIZATION_TOKEN:-}",
        "AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE": "${AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE:-}",
        "AWS_CONTAINER_CREDENTIALS_FULL_URI": "${AWS_CONTAINER_CREDENTIALS_FULL_URI:-}",
        "AWS_CONTAINER_CREDENTIALS_RELATIVE_URI": "${AWS_CONTAINER_CREDENTIALS_RELATIVE_URI:-}",
        "AWS_DEFAULTS_MODE": "${AWS_DEFAULTS_MODE:-}",
        "AWS_DEFAULT_PROFILE": "${AWS_DEFAULT_PROFILE:-}",
        "AWS_DEFAULT_REGION": "${AWS_DEFAULT_REGION:-}",
        "AWS_DISABLE_REQUEST_COMPRESSION": "${AWS_DISABLE_REQUEST_COMPRESSION:-}",
        "AWS_EC2_METADATA_DISABLED": "${AWS_EC2_METADATA_DISABLED:-}",
        "AWS_EC2_METADATA_SERVICE_ENDPOINT": "${AWS_EC2_METADATA_SERVICE_ENDPOINT:-}",
        "AWS_EC2_METADATA_SERVICE_ENDPOINT_MODE": "${AWS_EC2_METADATA_SERVICE_ENDPOINT_MODE:-}",
        "AWS_EC2_METADATA_V1_DISABLED": "${AWS_EC2_METADATA_V1_DISABLED:-}",
        "AWS_ENABLE_ENDPOINT_DISCOVERY": "${AWS_ENABLE_ENDPOINT_DISCOVERY:-}",
        "AWS_ENDPOINT_URL": "${AWS_ENDPOINT_URL:-}",
        "AWS_ENDPOINT_URL_CLOUDWATCH_LOGS": "${AWS_ENDPOINT_URL_CLOUDWATCH_LOGS:-}",
        "AWS_ENDPOINT_URL_SIGNIN": "${AWS_ENDPOINT_URL_SIGNIN:-}",
        "AWS_ENDPOINT_URL_SSO": "${AWS_ENDPOINT_URL_SSO:-}",
        "AWS_ENDPOINT_URL_SSO_OIDC": "${AWS_ENDPOINT_URL_SSO_OIDC:-}",
        "AWS_ENDPOINT_URL_STS": "${AWS_ENDPOINT_URL_STS:-}",
        "AWS_EXECUTION_ENV": "${AWS_EXECUTION_ENV:-}",
        "AWS_IGNORE_CONFIGURED_ENDPOINT_URLS": "${AWS_IGNORE_CONFIGURED_ENDPOINT_URLS:-}",
        "AWS_LOGIN_CACHE_DIRECTORY": "${AWS_LOGIN_CACHE_DIRECTORY:-}",
        "AWS_MAX_ATTEMPTS": "${AWS_MAX_ATTEMPTS:-}",
        "AWS_PROFILE": "${AWS_PROFILE:-}",
        "AWS_REGION": "${AWS_REGION:-}",
        "AWS_REQUEST_CHECKSUM_CALCULATION": "${AWS_REQUEST_CHECKSUM_CALCULATION:-}",
        "AWS_REQUEST_MIN_COMPRESSION_SIZE_BYTES": "${AWS_REQUEST_MIN_COMPRESSION_SIZE_BYTES:-}",
        "AWS_RESPONSE_CHECKSUM_VALIDATION": "${AWS_RESPONSE_CHECKSUM_VALIDATION:-}",
        "AWS_RETRY_MODE": "${AWS_RETRY_MODE:-}",
        "AWS_ROLE_ARN": "${AWS_ROLE_ARN:-}",
        "AWS_ROLE_SESSION_NAME": "${AWS_ROLE_SESSION_NAME:-}",
        "AWS_S3_DISABLE_EXPRESS_SESSION_AUTH": "${AWS_S3_DISABLE_EXPRESS_SESSION_AUTH:-}",
        "AWS_S3_DISABLE_MULTIREGION_ACCESS_POINTS": "${AWS_S3_DISABLE_MULTIREGION_ACCESS_POINTS:-}",
        "AWS_S3_USE_ARN_REGION": "${AWS_S3_USE_ARN_REGION:-}",
        "AWS_SDK_UA_APP_ID": "${AWS_SDK_UA_APP_ID:-}",
        "AWS_SECRET_ACCESS_KEY": "${AWS_SECRET_ACCESS_KEY:-}",
        "AWS_SECRET_KEY": "${AWS_SECRET_KEY:-}",
        "AWS_SESSION_TOKEN": "${AWS_SESSION_TOKEN:-}",
        "AWS_SHARED_CREDENTIALS_FILE": "${AWS_SHARED_CREDENTIALS_FILE:-}",
        "AWS_USE_DUALSTACK_ENDPOINT": "${AWS_USE_DUALSTACK_ENDPOINT:-}",
        "AWS_USE_FIPS_ENDPOINT": "${AWS_USE_FIPS_ENDPOINT:-}",
        "AWS_WEB_IDENTITY_TOKEN_FILE": "${AWS_WEB_IDENTITY_TOKEN_FILE:-}",
        "HOME": "${HOME:-}"
      },
      "tool": "collect",
      "behavior": {
        "syncable": true,
        "summarizable": true,
        "embeddable": true,
        "bm25_fields": [
          "summary",
          "content"
        ]
      },
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
            "resource_type"
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
            "resource_type"
          ],
          "edge_fields": [
            "from_id",
            "to_id"
          ]
        }
      }
    }
  }
}
```

The block above is generated: a test renders it from the same `ExampleEntry` the
collector ships and asserts this README carries it, so the two cannot drift. The
`env` block is complete rather than abbreviated, and it has to be — the
collector's process receives exactly the names the entry lists and nothing else,
so a name left off is a capability the credential chain does not have. `HOME` is
the one worth pointing at: without it no profile, single-sign-on session or
assumed role from the shared credentials and config files can be found at all.
Set the values in your own environment and the references carry them in, so no
credential is written into a config file; change the `command` to wherever you
installed the binary.

The `:-` in each reference is not decoration. A reference that is unset and
carries no default REFUSES THE ENTRY it sits in when the file is read, naming the
file, the entry, the field and the variable — the other collectors in that file
are unaffected, but this one does not collect. Almost every name here is
optional, and `${VAR:-}` resolves an unset one to the empty string, which is the
same input the AWS credential chain sees when the variable is simply absent, so
the defaulted form is what lets one block list every name at once.

**A credential you actually use is better written bare**, as
`"AWS_SECRET_ACCESS_KEY": "${AWS_SECRET_ACCESS_KEY}"`. Then a serving process
that does not hold the name refuses this entry and says so, rather than handing
the credential chain an empty key it reports as a permissions failure. That is
the shape the install script writes for a credential name your installing shell
holds.

### The `context` block, and what each entry is for

The `context` key is keyed by graph family and declares what this collector needs
from other graphs. The client fills exactly that and nothing else, and it refuses
a family this daemon has no collector registered for, naming it and listing the
ones it can supply — so delete an entry for a provider you do not run.

Both entries serve **EMITTED_BY** and confirmed **CORRELATES_WITH**. A log
group's service label is resolved to a cloud resource by NAME and ranked by that
resource's `resource_type` metadata key, which is why the entries ask for `id`,
`symbol_name` and `resource_type`; the `from_id` / `to_id` edge fields are what
CONFIRM a temporal correlation between two templates, so a declaration without
them resolves labels and confirms nothing. `cloud-resource` is the one node type
the aws and k8s collectors each emit — the provider's own resource kind rides in
`resource_type` rather than in the node type.

**`azure` and `gcp` are deliberately absent, for two different reasons.** The
service and namespace prefix lists this collector ranks on name no Azure type at
all, so an azure entry would be admitted and would resolve nothing; widening
those lists is a change to the resolution rather than to the declaration. The
gcp collector emits its resource type AS the node type — forty-nine
`gcp:<service>:<kind>` values plus a `gcp-cidr-block` sentinel — and a
declaration selects a type only by naming it exactly, so gcp waits for an
explicit family-level selector rather than shipping a forty-nine-entry list or a
key that selects nothing.

There is no `graph` key and no `reason` key. The family is the map key, and the
loader refuses an unknown field by name — which fails the whole config file, not
just the entry that carried it.

Two things the list deliberately leaves out, because they are not this
collector's to decide: the certificate-bundle variables and the proxy
variables. If your host keeps its certificate bundle somewhere unusual, or
reaches the internet through a proxy, add those names to the entry too.
Otherwise the collector will fail to reach CloudWatch, and it will say so.

## Running a collect

The tool takes the collect identifier, which names the graph instance, and its
own parameters:

```jsonc
{
  "id": "prod-us-east-1",
  "params": {
    "log_groups": ["/ecs/prod/api-server", "/aws/lambda/checkout"],
    "start_time": "2026-03-01T12:00:00Z",
    "end_time": "2026-03-01T13:00:00Z",
    "severity_min": "WARN"
  }
}
```

Only `log_groups` is required. Both time bounds are optional; with neither, the
walk covers everything the group still retains.

| Parameter | Meaning |
| --- | --- |
| `log_groups` | The log groups to walk. Required. |
| `start_time`, `end_time` | RFC 3339 bounds. |
| `filter_pattern` | A CloudWatch filter pattern, applied by CloudWatch. |
| `text_filter` | Keep entries whose message contains this text. Applied after the message is unwrapped, so it matches what you would read. |
| `severity_min` | Keep entries at or above this level. CloudWatch cannot order levels, so this is applied here. |
| `max_entries` | Stop after this many entries. Reaching it makes the walk incomplete; naming a bound the walk never reaches does not. There is no default and no ceiling: leave the key out and the walk drains every named group to the end, name any positive number and it is honored whatever its size. Zero and negative numbers are errors, because a caller asking for no entries and a caller asking for no bound have said two different things. |
| `region` | The AWS region. Absent means whatever the credential chain resolves. |

## Credentials

Credentials come from the AWS default credential chain and from nowhere else.
There is no key parameter and no profile parameter: nothing secret rides the
collect. If the chain resolves nothing, the collect fails and names the chain.

## Completeness

Every result says whether the walk enumerated its source. A walk that paged to
the end of every named group is complete, whether or not you bounded the window
— a narrower window is a smaller source, not a partial walk. Reaching
`max_entries` makes it incomplete, and the result says which bound cut it short.

An error anywhere fails the whole collect and writes nothing. There is no
partial result: a half-read log group reported as a complete walk would tell the
graph that everything the walk missed had been deleted.

## What it declares about itself

This collector serves the contract's required `describe` tool: one document
naming its suggested behavior and field lists, its five log node types and its edge types, the environment
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
knowledge-collector-cloudwatch --describe-tool        # the tool name a config entry's `tool` field takes
knowledge-collector-cloudwatch --describe-env-table   # one `class name` row per declared variable
```

## Linking to a cloud graph

A collect can carry a declared block of cloud resources and the edges between
them. The collector matches its service labels against the resources and emits a
proxy node per resolved resource, with an `EMITTED_BY` edge from the label to the
proxy.

It then looks for pairs of error templates in two different services whose time
ranges overlap, and emits a `CORRELATES_WITH` edge for each pair the declared
edges confirm depends on one another. Two services logging errors at the same
moment is a coincidence until the cloud graph says their resources are connected,
so an unconfirmed pair produces no edge at all. The detection is shared with
every other log collector rather than written here: it lives in
`cmd/collectors/common/correlation`.

With no block declared the collector emits none of the three, which is the same
thing the built-in log pipeline does without a cloud graph attached.
