# Azure collector

A knowledge collector for an Azure subscription. It serves one MCP tool over
stdio, walks the subscription through the Azure Resource Manager APIs, and
returns the subscription's resources and the relationships between them as a
graph.

It authenticates only through the Azure default credential chain, reading the
environment its config entry gives it. It is passed no credential by anything.

## What it collects

Every resource becomes one node of type `cloud-resource`, keyed by its Azure
resource id verbatim, carrying its Azure resource type under the metadata key
`resource_type` and its region under `region`. Fifty-one Azure resource types
are walked, across compute, networking, identity, storage, data, messaging, web
and monitoring.

Some relationships name something Azure has no resource id for: an address range
in a security rule, a certificate authority, a queue named by a function's
trigger binding, a workload federated from outside Azure. Those become PROXY
nodes, carrying `collected: "false"` with the reason they were not enumerated
and what referenced them, so a gap in coverage reads differently from a gap in
the source data.

Twenty-eight relationship types are drawn. Four of them can only be derived
after the whole walk has run — reachability from security rules, cross-tenant
trust, container image lineage, and grants that terminate on a directory group —
and this collector derives them inside its own walk, because nothing enriches a
collector's graph afterwards.

## Installing it

Released builds live in the public **knowledge-contrib** repository: every
release there carries one archive per collector per platform, named
`knowledge-collector-azure-<os>-<arch>.tar.gz` (`.zip` on Windows) and holding a
single binary called `knowledge-collector-azure`, plus a `checksums.txt` covering
all of them. The install script in that repository is the way to get one: it
picks the archive for your platform, verifies it against `checksums.txt` and
installs nothing if the checksum is missing or does not match. Building from
source stays supported and is described below.

The one command is:

```bash
curl -fsSL https://raw.githubusercontent.com/fulminate-io/knowledge-contrib/main/install.sh | sh -s -- azure
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
curl -fsSL https://raw.githubusercontent.com/fulminate-io/knowledge-contrib/main/uninstall.sh | sh -s -- azure
```

Or write the registration yourself.

Install or build the binary, then write a config entry naming it.

Registration is a config file: `~/.knowledge/collectors.json` for this machine's
operator, or `<repository root>/.knowledge/collectors.json` for one project.
`knowledge collector add` writes the entry for you:

```
knowledge collector add --tool collect azure -- /home/you/.knowledge/bin/knowledge-collector-azure
```

Options come before the name and the command follows the `--`, mirroring
`claude mcp add`. Repeat `-e KEY=VALUE` for each environment name the entry must
declare, and add `--summarizable=true` / `--embeddable=true` to opt into LLM
summaries and vectors.

The entry below is the one this collector's own tests generate, for a Linux host:
```json
{
  "azure": {
    "type": "stdio",
    "command": "/home/you/.knowledge/bin/knowledge-collector-azure",
    "tool": "collect",
    "behavior": {
      "summarizable": true,
      "embeddable": true,
      "syncable": true,
      "bm25_fields": ["summary", "symbol_name", "content"]
    },
    "env": {
      "AZURE_ADDITIONALLY_ALLOWED_TENANTS": "",
      "AZURE_AUTHORITY_HOST": "",
      "AZURE_CLIENT_CERTIFICATE_PASSWORD": "",
      "AZURE_CLIENT_CERTIFICATE_PATH": "",
      "AZURE_CLIENT_ID": "",
      "AZURE_CLIENT_SECRET": "",
      "AZURE_CLIENT_SEND_CERTIFICATE_CHAIN": "",
      "AZURE_FEDERATED_TOKEN_FILE": "",
      "AZURE_PASSWORD": "",
      "AZURE_POD_IDENTITY_AUTHORITY_HOST": "",
      "AZURE_REGIONAL_AUTHORITY_NAME": "",
      "AZURE_SDK_GO_LOGGING": "",
      "AZURE_SUBSCRIPTION_ID": "",
      "AZURE_TENANT_ID": "",
      "AZURE_TOKEN_CREDENTIALS": "",
      "AZURE_USERNAME": "",
      "DEFAULT_IDENTITY_CLIENT_ID": "",
      "HOME": "",
      "HTTPS_PROXY": "",
      "HTTP_PROXY": "",
      "IDENTITY_ENDPOINT": "",
      "IDENTITY_HEADER": "",
      "IDENTITY_SERVER_THUMBPRINT": "",
      "IMDS_ENDPOINT": "",
      "MSAL_FORCE_REGION": "",
      "MSI_ENDPOINT": "",
      "MSI_SECRET": "",
      "NO_PROXY": "",
      "PATH": "",
      "REGION_NAME": "",
      "SSL_CERT_DIR": "",
      "SSL_CERT_FILE": "",
      "http_proxy": "",
      "https_proxy": "",
      "no_proxy": ""
    }
  }
}
```

Every variable in the `env` block is left empty above; fill in the ones your
host needs. The block is the collector's WHOLE environment: nothing is inherited
from the daemon that spawns it, so a variable absent from the block is absent in
the collector, indistinguishable from one you never set.

## What each variable does

The credential chain and the transport beneath it read these. The list is
asserted against a census of the code that reads them, in both directions, so a
name here has a reader and a reader here has a name.

| Variable | What it does |
| --- | --- |
| `AZURE_SUBSCRIPTION_ID` | the subscription to walk, when the collect id and the tool's params name none |
| `AZURE_TENANT_ID` | the Entra tenant the service-principal and user credentials authenticate against |
| `AZURE_CLIENT_ID` | the application id of the service principal, or the user-assigned managed identity |
| `AZURE_CLIENT_SECRET` | the service-principal secret |
| `AZURE_CLIENT_CERTIFICATE_PATH` | the certificate file, for certificate authentication |
| `AZURE_CLIENT_CERTIFICATE_PASSWORD` | that file's password, when it is encrypted |
| `AZURE_CLIENT_SEND_CERTIFICATE_CHAIN` | sends the whole chain, which subject-name/issuer authentication requires |
| `AZURE_USERNAME` / `AZURE_PASSWORD` | the resource-owner-password arm of the chain |
| `AZURE_ADDITIONALLY_ALLOWED_TENANTS` | extra tenants a credential may acquire tokens for |
| `AZURE_AUTHORITY_HOST` | the Entra authority host, which a sovereign cloud changes |
| `AZURE_TOKEN_CREDENTIALS` | selects which arms of the default chain are built |
| `AZURE_FEDERATED_TOKEN_FILE` | the projected token workload identity exchanges |
| `AZURE_REGIONAL_AUTHORITY_NAME` | pins the token authority region |
| `IDENTITY_ENDPOINT`, `IDENTITY_HEADER` | the managed-identity endpoint injected by App Service, Container Apps and Arc, and its secret header |
| `IDENTITY_SERVER_THUMBPRINT` | the Service Fabric managed-identity server thumbprint |
| `IMDS_ENDPOINT` | the Azure Arc instance-metadata endpoint |
| `MSI_ENDPOINT`, `MSI_SECRET` | the legacy App Service managed-identity endpoint and its secret |
| `AZURE_POD_IDENTITY_AUTHORITY_HOST` | the pod-identity host; declared for completeness and read by nothing at the pinned SDK versions |
| `DEFAULT_IDENTITY_CLIENT_ID` | the client id a system-assigned-by-default host injects |
| `MSAL_FORCE_REGION`, `REGION_NAME` | region selection for the token authority; **absent, these change behavior silently** |
| `AZURE_SDK_GO_LOGGING` | turns on the Azure SDK's own logging, which goes to stderr |
| `HTTPS_PROXY`, `HTTP_PROXY`, `NO_PROXY` and their lowercase spellings | the proxy this collector reaches the management plane through; the resolution is memoized at the first request, so the entry decides it |
| `PATH` | how the Azure CLI, Azure Developer CLI and Azure PowerShell arms find the tool they shell out to |
| `HOME` | where those arms find their cached credentials, on Linux and macOS (`AZURE_CONFIG_DIR` replaces it rather than adding to it) |
| `SSL_CERT_FILE`, `SSL_CERT_DIR` | the CA bundle, on Linux only; macOS verifies through the system keychain and Windows through its own store |
| `SYSTEMROOT` | **Windows only**: where the Windows developer-credential arm finds the shell it runs; without it that arm reports itself unavailable |
| `ProgramData` | **Windows only**: where the Azure Arc managed-identity path looks for its key file |

### What the entry deliberately leaves out

These are read somewhere in this collector's dependency closure and are not
declared, each with what omitting it costs.

| Variable | Cost of leaving it out |
| --- | --- |
| `OS` | the SDK composes a User-Agent token from it on Windows; you get a generic token and nothing else changes |
| `REQUEST_METHOD` | the proxy resolver treats its presence as a CGI environment, so declaring it would change behavior rather than enable any |
| `GOOGLE_APPLICATION_CREDENTIALS` | read by an OAuth library in the closure; this collector builds no Google credential, so nothing reads it on any path taken here |
| `SYSTEM_OIDCREQUESTURI` | read by the Azure Pipelines credential, which is not one of the arms the default chain builds |
| `HOSTALIASES`, `LOCALDOMAIN`, `RES_OPTIONS` | their only effect is to select the cgo resolver; omitting them preserves the default choice rather than degrading it |
| `PATHEXT` | Windows executable-suffix search; absent, it falls back to the compiled-in list and still resolves the shell |
| `APPDATA`, `USER` | read only behind a build constraint this collector never builds under |

## The tool

One tool, `collect`. Its input is the collect id — the subscription — and an
optional `params` object:

| Params key | What it does |
| --- | --- |
| `subscription_id` | walks this subscription instead of the one the collect id names |
| `max_concurrency` | how many per-service walks run at once; 0 means the built-in bound |

No params key is a credential, and none is required: a collect that carries no
params at all walks the subscription its id names.

## What it does when something goes wrong

Nothing is swallowed and nothing degrades quietly.

- **No credential** in the chain, or no subscription in the id, the params or
  the environment: the collect is refused with an error naming what was missing
  and where to set it. Nothing is written.
- **One service fails** mid-walk: the other services still run, the resources
  already gathered are returned, and the walk asserts itself INCOMPLETE naming
  every service that failed. That assertion is what stops the server treating
  the resources this run could not read as deleted.
- **A relationship pass fails**: the same, as an incomplete walk naming the
  pass. It is never logged and forgotten.

## What it declares about itself

This collector serves the contract's required `describe` tool: one document
naming its suggested behavior and field lists, its one node type and its edge types, the environment
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
knowledge-collector-azure --describe-tool        # the tool name a config entry's `tool` field takes
knowledge-collector-azure --describe-env-table   # one `class name` row per declared variable
```

## Building and testing

```
cd cmd/collectors/azure
go build ./...
go test ./...
```

The tests need no Azure subscription: every walk is driven from hand-built API
response values, and the process-level tests spawn this module's own test binary
rather than talking to Azure.
