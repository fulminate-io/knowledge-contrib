# bitbucket-pipelines collector

A knowledge collector for Bitbucket Pipelines. It serves one tool over MCP on
stdio: a daemon spawns it, calls that tool with a collect id naming a Bitbucket
workspace, and writes what it returns into a graph of its own.

It reads. Every call it makes is a GET, and it holds no operation that could
change anything in a workspace.

## What it produces

One node per resource and one edge per relationship, for **nine resource kinds**
across a workspace's CI/CD:

| Kind | What it is |
|---|---|
| workspace | the workspace the collect names |
| repository | each repository in it |
| pipeline | each pipeline definition in each repository's `bitbucket-pipelines.yml` |
| pipeline_run | the most recent runs of each repository |
| runner | each runner, at the workspace and at each repository |
| label | each distinct runner label |
| environment | each deployment environment |
| approval_gate | the deployment restriction an environment carries, where it has one |
| variable | each pipeline variable NAME, at the workspace, the repository and each deployment environment |

Every node carries the contract's single node type with its kind in the
`resource_type` metadata key, which is the shape a consumer queries on.

**Six relationship types.** `BELONGS_TO` from every child to its parent, at six
sites; `DEPLOYS_TO` from a pipeline to the environment its step deploys to;
`RUNS_IN` from a pipeline to each runner label its step asks to run on;
`USES_SECRET` from a pipeline to each variable its script references;
`HAS_LABEL` from a runner to each label it carries; and `REQUIRES_APPROVAL` from
an environment to its approval gate.

There is no TRIGGERED_BY. A pipeline run carries a trigger TYPE and names no
other resource, so an edge asserting one would be invented; a test asserts its
absence, because a coverage check that only looks for what should be there never
notices what should not.

### Node identity

A node's id is the provider's own: `bitbucket:<workspace>/<Kind>/<name>`, with
the repository's slug in the middle for anything that belongs to one. A variable
carries its SCOPE in the id as well: `workspace/<key>`, `repository/<slug>/<key>`
or `env/<slug>/<environment>/<key>`.

### Every edge points at a node this collector emitted

Two node classes exist, and three edge classes are resolved before they are
emitted, so that stays true.

**One label node per distinct label name**, not one per runner: the id carries
the workspace and the name and nothing else, so two runners sharing `self-hosted`
are two edges into one node, and a pipeline step's `runs-on` reaches the same
node the runner does.

**An approval gate** is a node rather than a field, so the restriction an
environment carries is a thing in the graph that a traversal can reach.

**Three edge classes are resolved against the walk's own nodes**, because all
three are names read out of a repository's `bitbucket-pipelines.yml` while the
things they name come from the API through other enumerations.

A step's `$VAR` is resolved most specific scope first: the step's deployment
environment, then the repository, then the workspace. A step's `deployment` is
resolved against the environments that repository declares. A step's `runs-on`
label is resolved against the labels the workspace's runners advertise.

Where nothing satisfies one, **no edge is emitted and the name is recorded** on
the pipeline node, under `unresolved_variable_refs`, `unresolved_deployments` or
`unresolved_runs_on` — an omission you can see rather than an edge pointing at
nothing. A step deploying to an environment nobody created, or asking for a
runner label none advertises, is an ordinary workspace configuration; the
alternative of minting the node would assert that a deployment target or a runner
class exists when the provider says it does not.

Because a refused or failed environments, runners or variables read marks the
whole collect INCOMPLETE, a name recorded on a COMPLETE collect means the thing
really is not there.

## Building it

```
cd cmd/collectors/bitbucket-pipelines
go build -o bitbucket-pipelines .
```

The binary has no flags and no configuration of its own. Everything it needs
arrives on the tool call or in its environment.

## Installing it

Released builds live in the public **knowledge-contrib** repository: every
release there carries one archive per collector per platform, named
`knowledge-collector-bitbucket-pipelines-<os>-<arch>.tar.gz` (`.zip` on Windows)
and holding a single binary called
`knowledge-collector-bitbucket-pipelines`, plus a `checksums.txt` covering all of
them. The install script in that repository is the way to get one: it picks the
archive for your platform, verifies it against `checksums.txt` and installs
nothing if the checksum is missing or does not match. Building from source stays
supported and is described above.

**The install script writes no provider credential value.** This collector
authenticates with a Bitbucket username and app password, and neither VALUE is
written into a config file. What the entry carries is a reference to each
variable — `"BITBUCKET_USERNAME": "${BITBUCKET_USERNAME}"` and the same for the
app password — written for whichever of them your installing shell holds. The
values are read from the environment the daemon serves collects from, at the
moment it starts this collector; if that process does not have a name, the
collect is refused naming it, and no other collector in your config file is
affected.

A collector is installed by writing an entry into a JSON config file. There is no
registration command and no server-side record: the entry IS the registration,
and removing it unregisters the collector at the next lookup.

The file is `collectors.json`, in either of two scopes:

- `~/.knowledge/collectors.json` for this machine's operator;
- `<repository root>/.knowledge/collectors.json` for one project.

Where a name is in both, the project entry wins, and it wins WHOLE: no field is
merged across scopes.

The entry's key is the graph family the results land in, so this one is
`bitbucket-pipelines`. `knowledge collector add` writes it for you, mirroring
`claude mcp add` — options before the name, the command after the `--`:

```
knowledge collector add --tool collect bitbucket-pipelines -- /usr/local/bin/knowledge-collector-bitbucket-pipelines
```

Add `--summarizable=true` / `--embeddable=true` to opt into LLM summaries and
vectors. The same entry written by hand:

```json
{
  "collectors": {
    "bitbucket-pipelines": {
      "type": "stdio",
      "command": "/usr/local/bin/knowledge-collector-bitbucket-pipelines",
      "env": {
        "BITBUCKET_APP_PASSWORD": "${BITBUCKET_APP_PASSWORD}",
        "BITBUCKET_USERNAME": "${BITBUCKET_USERNAME}"
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

Replace `command` with where you put the binary. The loader is strict: a key it
does not know, a missing `type` or a missing `tool` is a refusal naming the file,
the entry and the field, not an entry it quietly skips.

**Give `command` an absolute path.** A stdio entry's command is resolved against
the DAEMON's `PATH`, not your shell's, and a daemon started by a service manager
does not inherit your login shell's environment.

## The behavior block is what makes the graph searchable

The three booleans are the pipeline's switches. Two of them — `summarizable` and
`embeddable` — default to FALSE, so a graph registered without the block is
collected, readable by id and walkable, and never summarized or embedded.

The three FIELD LISTS have no default at all. A graph whose lists are empty
carries nothing into the text index: searching it reads zero and keeps reading
zero. Each list names node fields this collector actually populates.

| List | Fields | Why |
|---|---|---|
| `embed_fields` | summary, symbol_name | the sentence and the name; the content is a JSON document and embedding one spends the vector on punctuation |
| `summarize_fields` | content, symbol_name, summary | the resource's actual configuration is what there is to summarize |
| `bm25_fields` | symbol_name, summary, content | a keyword search for a pipeline name, a variable name or a runner label finds it in the content and nowhere else |

There is no `node_types` block. This collector emits one node type with the kind
in metadata, so there is nothing to override per type.

## The environment block carries one key at most

A stdio collector receives EXACTLY the environment its entry declares. The daemon
copies nothing from its own and adds nothing, so a variable you do not list is a
variable this collector does not have.

This collector's whole closure is three names, in two classes:

| Name | Class | Why |
|---|---|---|
| `BITBUCKET_USERNAME` | credential | the account the app password belongs to |
| `BITBUCKET_APP_PASSWORD` | credential | the app password itself |
| `BITBUCKET_PIPELINE_HISTORY_DEPTH` | selector | how many recent pipeline runs to read per repository; unset means 50 |

**Neither credential VALUE is written into the entry above, and a value is not
what belongs there.** What the entry names is the two variables:
`"BITBUCKET_USERNAME": "${BITBUCKET_USERNAME}"` and the same shape for the app
password are references to your own environment, expanded by the process that
serves the collect at the moment it starts this collector. Set both in the
environment the daemon serves from and they reach this collector's child process
from there, having been in no file on the way. Both halves are required together:
an app password without its username authenticates nothing.

Export both in the shell you run the install script in too: the script writes a
reference only for a credential name that shell holds, and prints the names it
left out when it does not. If you installed without them, export
`BITBUCKET_USERNAME` and `BITBUCKET_APP_PASSWORD` and re-run the script.

**Use `${BITBUCKET_APP_PASSWORD}` and not `${BITBUCKET_APP_PASSWORD:-}`.** The
second is a default: in a process that does not hold the name it resolves to the
empty string, and this collector is then handed a credential that is present and
empty, which it reports as a missing one. The first has nothing to fall back to,
so the entry is refused naming the file, the entry, the key and the variable —
and only that entry: every other collector in the same config file keeps
collecting.

The selector is the one key an installer writes as a LITERAL, and only when it is
set in the installing shell. Unset, the key is omitted entirely rather than
written empty.

With either credential unset or empty, the collect fails naming BOTH. It does not
fall back to an unauthenticated read: an unauthenticated read of a workspace
returns a fraction of what a member sees and none of what this collector is for,
so it would land a graph that looks like a small workspace rather than a failed
collect.

A variable set to the EMPTY STRING is present and empty, which a stdio child can
tell apart from absent. This collector treats an empty credential as a missing
one, and it REFUSES an empty history depth rather than falling back to the
default: the empty string names no number, and a value it cannot use is an error
rather than something to work around silently. A non-integer, a zero and a
negative are refused on the same terms, naming the variable, quoting what was
sent and naming the default.

**No variable value reaches the graph.** Variables appear as a key, a scope and a
secured flag and nothing else — the decoder declares no field for a value, so one
is dropped before it is ever a string this collector holds. A test asserts over
the raw bytes of a whole collect result that a planted value appears in none of
them.

## Collecting

```json
{"id": "acme", "params": {}}
```

The `id` is the Bitbucket workspace to enumerate, and it is also the graph the
results land in. This collector takes no parameter naming a workspace and reads
no environment variable to resolve one, so a call without an id is refused rather
than walked against something else.

One optional parameter:

| Parameter | Default | Maximum |
|---|---|---|
| `max_concurrency` | 10 | 32 |

It bounds how many enumerations run at once. A value above the maximum is REFUSED
naming the parameter and the ceiling, rather than clamped and reported as what
you asked for.

The history depth is NOT a parameter. It already has an environment home, and one
value with two sources would be a precedence question with no right answer.

## Anything this collector did not see is reported as not seen

Every collect asserts whether it enumerated the whole workspace, and that
assertion decides whether the server may treat what the collect did not carry as
deleted.

| Outcome | What happened | What you do |
|---|---|---|
| refused | the app password may not read one repository's variables, runners or environments | grant the permission |
| rate limited | the provider answered 429 to every one of four attempts on one read | collect less often, or lower the history depth |
| partial | the provider did not return one repository, one environment or one pipeline definition, or a read exceeded the 30-second timeout | usually nothing; it is the provider's and it passes |

None of the three fails the collect. Every other repository is still walked and
everything they found is still returned, including whatever page a refused,
rate-limited or partial read had already collected. What changes is the mark on
the collect.

A 404 on a listing is a partial read, and the reason quotes the URL. Bitbucket
reports a feature nobody configured by answering the listing with an empty page,
not with a 404 — a workspace with no variables, a repository with no runners and
a repository that has never run a pipeline all answer 200 — so a 404 means the
URL was not there: a wrong path, a scope this credential cannot address, a
repository that is gone. Reading it as an empty scope is how a repository-scope
variables path that 404'd on every request went unnoticed through this
collector's whole life, with every collect reporting a complete walk of a
workspace whose variables it had never read.

A 404 on a repository's pipeline definition is not, and it is the only 404 that
is not. `bitbucket-pipelines.yml` is a file rather than a listing, most
repositories do not have one, and marking that incomplete would leave nearly
every workspace permanently marked.

WHY THE INCOMPLETE MARK MATTERS MORE THAN IT LOOKS. A COMPLETE collect lets the
receiving server treat everything the collect did not carry as gone. A collector
that reported an unreadable repository as an ordinary empty result would
therefore, on the next collect, delete that repository's whole inventory and
report success.

If the repository listing itself fails, the collect fails outright: five of the
six enumerations start from that list, so a walk that could not read it never
found out what the workspace contains.

## Two collects of an unchanged workspace produce the same result

The output is sorted by node id and by edge, and one node is emitted per id, so a
second collect of a workspace nothing changed is identical to the first, byte for
byte. That is not automatic here: the pipeline definition's trigger sections are
YAML maps, and the enumerations merge concurrently, so both orders vary run to
run. A diff between two collects shows what moved in your CI/CD and nothing else.

## Running its tests

```
cd cmd/collectors/bitbucket-pipelines
go test ./...
```

No credential and no network. This collector has no provider SDK, so its tests
stand a real HTTP server in for the Bitbucket API: the pagination, the retry
budget, the `Retry-After` parse, the cancellation arm and every per-status
outcome are exercised against it. The recorded responses are assembled into one
workspace so a single walk reaches the whole declared vocabulary, and the suite
asserts that the collector emits every resource kind and every relationship it
declares.
