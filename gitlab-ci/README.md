# gitlab-ci collector

A knowledge collector for GitLab CI/CD. It serves one MCP tool over stdio; a
daemon spawns it, calls that tool with a collect id naming a GitLab group, and
writes what it returns into a graph of its own.

It reads and never writes. Every call it makes is a list or a get, and it holds no
operation that could change anything in a group.

## What it produces

One walk of a group enumerates seven things, all of them behind a single project
discovery:

| Enumeration | What it reads |
|---|---|
| `gitlab-projects` | the group, its projects, and the projects of every subgroup |
| `gitlab-pipelines` | each project's `.gitlab-ci.yml` on its default branch |
| `gitlab-pipeline-runs` | each project's most recent pipeline runs, and their jobs |
| `gitlab-runners` | the group's runners and each project's, with their tags |
| `gitlab-environments` | each project's environments and its protection rules |
| `gitlab-deployments` | each project's most recent deployments |
| `gitlab-variables` | CI/CD variable NAMES at the group and at each project |

Eleven kinds of resource come out of that: `group`, `project`, `pipeline`,
`pipeline-run`, `job`, `runner`, `runner-tag`, `environment`, `protection-rule`,
`deployment` and `variable`. They are all one node type — `cicd-resource` — with
the kind in the `resource_type` metadata key, which is the shape a consumer
already queries.

Six kinds of relationship join them: `BELONGS_TO`, `DEPLOYS_TO`, `RUNS_IN`,
`USES_SECRET`, `HAS_LABEL` and `REQUIRES_APPROVAL`.

### A variable's value is never read

A GitLab CI/CD variable is a secret store. This collector reads variable KEYS,
their scope and their protected and masked flags, and nothing else: a variable
node carries no content at all, and the interface these enumerations run through
carries the provider's LIST call and not its per-variable get, so there is no call
available to this code that could return a value.

### Node identity

Every node id is `gitlab:<group>/<Kind>/<path or id>`, carried over verbatim from
the built-in collector this module replaces so that a consumer's stored queries
and saved traversals keep working. A group-scoped variable and a project-scoped
one differ only in the segment naming their owner.

### Runner tags are nodes, and there is one per distinct tag

A runner's tags are materialized as `runner-tag` nodes so that every `HAS_LABEL`
edge resolves. There is ONE node per distinct tag name across the whole walk, not
one per runner: two runners carrying `docker` are two edges into one node, at
either scope.

### Three edges can still point at nothing, and that is data-dependent

- A pipeline referencing a GROUP-scoped variable. The document names the variable
  by name alone, so the edge is built at project scope; a node for that variable
  exists in the same result under the group's own owner segment.
- A job asking for a runner tag no discovered runner carries. That is the truthful
  record of a job that cannot be scheduled.
- A protected environment the environments listing did not return. This is the one
  relationship in the graph whose dangling side is its SOURCE.

None of the three is a missing node CLASS: each depends on what a pipeline
document says or on what two separate API calls happened to return.

## Building it

```
cd cmd/collectors/gitlab-ci
go build -o knowledge-collector-gitlab-ci .
```

The binary takes no flags and reads no arguments beyond `--version`.

## Installing it

Released builds live in the public **knowledge-contrib** repository: every release
there carries one archive per collector per platform, named
`knowledge-collector-gitlab-ci-<os>-<arch>.tar.gz` (`.zip` on Windows) and holding
a single binary called `knowledge-collector-gitlab-ci`, plus a `checksums.txt`
covering all of them. The install script in that repository is the way to get one:
it picks the archive for your platform, verifies it against `checksums.txt` and
installs nothing if the checksum is missing or does not match. Building from
source stays supported and is described above.

**The install script writes no provider credential value.** Both of this
collector's token variables are credentials, and a credential's VALUE is never
written into a config file. What the entry carries is a reference to the
variable — `"GITLAB_TOKEN": "${GITLAB_TOKEN}"` — written for whichever of the two
token names your installing shell holds. The value is read from the environment
the daemon serves collects from, at the moment it starts this collector; if that
process does not have the name, the collect is refused naming it, and no other
collector in your config file is affected. The script also writes the instance
selector described below, as a literal, because it names a host rather than a
secret.

A collector is installed by writing an entry into a JSON config file. There is no
registration command and no server-side record: the entry IS the registration, and
removing it unregisters the collector at the next lookup.

The file is `collectors.json`, in either of two scopes:

- `~/.knowledge/collectors.json` for this machine's operator;
- `<repository root>/.knowledge/collectors.json` for one project.

Where a name is in both, the project entry wins, and it wins WHOLE: no field is
merged across scopes.

The entry's key is the graph family the results land in, so this one is
`gitlab-ci`. `knowledge collector add` writes it for you — options before the
name, the command after the `--`:

```
knowledge collector add --tool collect gitlab-ci -- /usr/local/bin/knowledge-collector-gitlab-ci
```

Add `--summarizable=true` / `--embeddable=true` to opt into LLM summaries and
vectors. The same entry written by hand:

```json
{
  "collectors": {
    "gitlab-ci": {
      "type": "stdio",
      "command": "/usr/local/bin/knowledge-collector-gitlab-ci",
      "env": {
        "GITLAB_TOKEN": "${GITLAB_TOKEN}",
        "GITLAB_URL": "https://gitlab.example.com/"
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

The family name is `gitlab-ci` and not `gitlab`. The shorter name belongs to a
collector compiled into the client, and registering a family under it would have
that collector's own post-collect step run over this graph.

## The behavior block is what makes the graph searchable

Two of the three booleans default to FALSE, and the three field lists have no
default at all — so an entry with no behavior block is collected, readable by id
and walkable, and never summarized, never embedded and absent from the text index.
The block above declares all six.

- `embed_fields` is the one-line summary and the resource's own name. A node's
  content is a JSON document or a YAML pipeline definition, and embedding one
  spends the vector on its punctuation.
- `summarize_fields` is the content, with the name and the existing summary for
  context: the resource's actual configuration is what a summarizer needs.
- `bm25_fields` is all three, because a keyword search for a project path, a
  variable name or a runner tag finds it in the content and nowhere else.

## The environment block carries the selector and the token by reference

This collector's whole environment closure is three names:

| Name | What it is |
|---|---|
| `GITLAB_TOKEN` | the API token, read first |
| `GITLAB_PRIVATE_TOKEN` | the same credential under its other common spelling, read when the first is unset or empty |
| `GITLAB_URL` | the instance to enumerate |

The two tokens are CREDENTIALS, and neither VALUE is written into the entry. What
the entry above carries is `"GITLAB_TOKEN": "${GITLAB_TOKEN}"`, a reference to
your own environment: a stdio collector receives exactly the environment its
entry declares, and the process serving the collect expands that reference when
it starts this collector. Set the variable in the environment the daemon serves
from, and the token reaches this collector having been in no file on the way.

Export it in the shell you run the install script in too: the script writes the
reference only for a credential name that shell holds, and prints the names it
left out when it does not. If you installed without it, export `GITLAB_TOKEN`
and re-run the script.

The two names are alternatives, so reference the one you set. Referencing both
means a collect is refused until both are set.

**Use `${GITLAB_TOKEN}` and not `${GITLAB_TOKEN:-}`.** The second is a default:
in a process that does not hold the name it resolves to the empty string, and
this collector is then handed a token that is present and empty, which it reports
as a missing token rather than as a variable you have not set. The first has
nothing to fall back to, so the entry is refused naming the file, the entry, the
key and the variable — and only that entry: every other collector in the same
config file keeps collecting.

`GITLAB_URL` is a SELECTOR, not a credential: it names the GitLab instance and
defaults to `https://gitlab.com/`. Set it to your own host to enumerate a
self-hosted instance, and delete the whole `env` block if you are on the hosted
one. A value carrying userinfo — a URL of the form `https://user:token@host` — is
refused rather than dialed, because it embeds a credential in a string this
collector hands to an HTTP client.

A variable that arrives PRESENT AND EMPTY is not the same as an absent one. For
the tokens, present-and-empty is a missing token and fails the collect by name.

## Collecting

```
collect(type: "gitlab-ci", id: "acme")
```

The id is the group's path, and a subgroup path such as `acme/platform` is an
ordinary id. It is also the graph the results land in: this collector neither
prefixes nor rewrites it.

Two optional parameters bound the two enumerations that read a project's history
rather than its current state:

| Parameter | Default | Maximum |
|---|---|---|
| `max_pipeline_runs` | 20 | 100 |
| `max_deployments` | 20 | 100 |

Both read a SINGLE page, so a value above the maximum is refused by name rather
than silently answered with 100, and a value below 1 is refused rather than read
as "use the default".

## Anything this collector did not see is reported as not seen

A refused or failed per-project read does not fail the collect and does not vanish
either: everything that was read is still returned, every other project is still
walked, and the walk is marked INCOMPLETE with a reason naming the enumeration and
the scope. That mark is what stops the receiving server from treating everything
the collect did not carry as deleted.

The same goes for a subgroup tree deeper than the ten levels this collector walks,
for a `.gitlab-ci.yml` that does not parse, and for a runner whose detail read
failed: each is reported rather than silently truncating the graph. A project with
no `.gitlab-ci.yml` at all is NOT an incompleteness — the provider answered, and
the answer is that the file is not there.

## Two collects of an unchanged group produce the same result

The walk fans out over its enumerations concurrently, and the graph builder sorts
and deduplicates, so two collects of a group that did not change produce
byte-identical nodes and edges. The project discovery is shared and ordered, which
is also what makes a runner visible to several projects belong to the same one
twice running.

## Running its tests

```
cd cmd/collectors/gitlab-ci
go test ./...
```

Every enumeration is exercised against recorded provider responses built from the
SDK's own types, so the whole collector runs offline with no credential and no
network.
