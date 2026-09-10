# github-actions collector

A knowledge collector for GitHub Actions. It serves one tool over MCP on stdio: a
daemon spawns it, calls that tool with a collect id naming a GitHub organization,
and writes what it returns into a graph of its own.

It reads. Every call it makes is a list, and it holds no operation that could
change anything in an organization.

## What it produces

One node per resource and one edge per relationship, across an organization's
CI/CD:

| Kind | What it is |
|---|---|
| organization | the organization the collect names |
| repository | each non-archived repository in it |
| workflow | each active workflow of each repository |
| workflow_run | the most recent runs of each repository |
| runner | each self-hosted runner, at the organization and at each repository |
| environment | each deployment environment, with its protection rules |
| deployment | the most recent deployments of each repository |
| secret | each secret NAME, at the organization, the repository and the environment |
| user | each person the walked objects name: a run's actor and triggering actor, a deployment's creator, and the reviewers an environment's protection rules require |
| team | the teams an environment's protection rules require as reviewers |
| label | each distinct runner label |

Every node carries the contract's single node type with its kind in the
`resource_type` metadata key, which is the shape a consumer queries on.

**The relationships.** `BELONGS_TO` from every child to its parent, at seven
sites; `TRIGGERED_BY` from a run to the repository, carrying the trigger event as
evidence; `DEPLOYS_TO` from a deployment and from a workflow to an environment;
`USES_SECRET` from a workflow to each secret its text names; `REQUIRES_APPROVAL`
from an environment to each required reviewer; and `HAS_LABEL` from a runner to
each label it carries.

Three more name a **person**. `ATTRIBUTED_TO` from a run to the user it is filed
under; `INITIATED_BY` from a run to the user whose action started that particular
run; and `CREATED_BY` from a deployment to the user who created it. A run's two
users are the same person on a first run and different people on a re-run, which
is the only thing in the provider's answer that says a re-run happened — so they
are two classes rather than one.

`TRIGGERED_BY` names the repository and not a person, and it stays that way. The
trigger event is a kind of occurrence rather than a thing with an identity, so
that edge says which kind fired and where; who caused it is at the other end of
one of the three above.

There is no `RUNS_IN`. Nothing this collector reads names the runner a run
executed on, so an edge asserting one would be invented; a test asserts its
absence, because a coverage check that only looks for what should be there never
notices what should not.

### Node identity

A node's id is the provider's own: `github:<org>/<Kind>/<name>`, with the
repository's `owner/name` in the middle for anything that belongs to one. An
organization-scoped secret is one segment shorter than a repository-scoped one,
because there is no repository to name.

### The people, and what their nodes carry

A `user` node is minted from every field of a walked object that names a person:
a run's actor and triggering actor, a deployment's creator, and an environment's
required reviewers. All four ride responses this collector already fetches, so
the whole class costs no extra call to the provider.

The node is keyed on the provider's own login and carries the person's numeric
id, whether they are a `User` or a `Bot`, and the provider URL. The login is what
every edge already joins on and what a reader recognizes; the numeric id is
carried beside it because it is the one that survives a rename.

**No address reaches the graph.** The provider's user object has an `email` field
and a run's head commit has an author with another. Neither is read: a person's
address is not inventory of an organization's CI/CD. A test asserts over the raw
bytes of a whole collect result that no address appears in any of them.

### Two more kinds of node exist so an edge resolves

The reviewer `team` nodes and the runner `label` nodes are minted from the
enumerations that already read them. Without them, every approval edge naming a
team and every label edge would name something no query returns and no traversal
can follow.

**One label node per distinct label name**, not one per runner: the id carries the
organization and the name and nothing else, so two runners sharing `self-hosted`
are two edges into one node.

### Two edges can still point at nothing, and that is data-dependent

A workflow's text names a secret by name alone and says nothing about which scope
it comes from, so the edge is built at repository scope and does not resolve for
an organization- or environment-scoped secret. A workflow's text can likewise name
an environment the environments API did not return, including one written as an
expression the provider evaluates when the workflow runs.

Both are left as they are rather than guessed at. A test asserts each stays
unresolved, so a later change to either is a decision on the record rather than a
silent difference in a consumer's graph.

## Building it

```
cd cmd/collectors/github-actions
go build -o github-actions .
```

The binary has no flags and no configuration of its own. Everything it needs
arrives on the tool call or in its environment.

## Installing it

Released builds live in the public **knowledge-contrib** repository: every release
there carries one archive per collector per platform, named
`knowledge-collector-github-actions-<os>-<arch>.tar.gz` (`.zip` on Windows) and
holding a single binary called `knowledge-collector-github-actions`, plus a
`checksums.txt` covering all of them. The install script in that repository is the
way to get one: it picks the archive for your platform, verifies it against
`checksums.txt` and installs nothing if the checksum is missing or does not match.
Building from source stays supported and is described above.

**The install script writes no provider credential value.** This collector's
whole environment is a GitHub token, and a token's VALUE is never written into a
config file. What the entry carries is a reference to the variable —
`"GITHUB_TOKEN": "${GITHUB_TOKEN}"` — written for whichever of the two token
names your installing shell holds. The value is read from the environment the
daemon serves collects from, at the moment it starts this collector; if that
process does not have the name, the collect is refused naming it, and no other
collector in your config file is affected.

A collector is installed by writing an entry into a JSON config file. There is no
registration command and no server-side record: the entry IS the registration, and
removing it unregisters the collector at the next lookup.

The file is `collectors.json`, in either of two scopes:

- `~/.knowledge/collectors.json` for this machine's operator;
- `<repository root>/.knowledge/collectors.json` for one project.

Where a name is in both, the project entry wins, and it wins WHOLE: no field is
merged across scopes.

The entry's key is the graph family the results land in, so this one is
`github-actions`. `knowledge collector add` writes it for you, mirroring
`claude mcp add` — options before the name, the command after the `--`:

```
knowledge collector add --tool collect github-actions -- /usr/local/bin/knowledge-collector-github-actions
```

Add `--summarizable=true` / `--embeddable=true` to opt into LLM summaries and
vectors. The same entry written by hand:

```json
{
  "collectors": {
    "github-actions": {
      "type": "stdio",
      "command": "/usr/local/bin/knowledge-collector-github-actions",
      "env": {
        "GITHUB_TOKEN": "${GITHUB_TOKEN}"
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
| `bm25_fields` | symbol_name, summary, content | a keyword search for a workflow path, a secret name or a runner label finds it in the content and nowhere else |

There is no `node_types` block. This collector emits one node type with the kind
in metadata, so there is nothing to override per type.

## The environment block is one reference, and no value

A stdio collector receives EXACTLY the environment its entry declares. The daemon
copies nothing from its own and adds nothing, so a variable you do not list is a
variable this collector does not have.

This collector's whole closure is two names, and both are credentials:

| Name | Why |
|---|---|
| `GITHUB_TOKEN` | the token, consulted first |
| `GH_TOKEN` | consulted when the first is unset or empty |

**Neither VALUE is written into the entry above, and a value is not what belongs
there.** What the entry names is the variable: `"GITHUB_TOKEN": "${GITHUB_TOKEN}"`
is a reference to your own environment, expanded by the process that serves the
collect at the moment it starts this collector. Set the variable in the
environment the daemon serves from and the token reaches this collector's child
process from there, having been in no file on the way.

Export it in the shell you run the install script in too: the script writes the
reference only for a credential name that shell holds, and prints the names it
left out when it does not. If you installed without it, export `GITHUB_TOKEN`
and re-run the script.

The two names are alternatives, so reference the one you set. Referencing both
means a collect is refused until both are set.

**Use `${GITHUB_TOKEN}` and not `${GITHUB_TOKEN:-}`.** The second is a default:
in a process that does not hold the name it resolves to the empty string, and
this collector is then handed a token that is present and empty, which it reports
as a missing token rather than as a variable you have not set. The first has
nothing to fall back to, so the entry is refused naming the file, the entry, the
key and the variable — and only that entry: every other collector in the same
config file keeps collecting.

With neither set to a non-empty value, the collect fails naming both. It does not
fall back to an unauthenticated read: an unauthenticated read of an organization
returns a fraction of what a member sees and none of what this collector is for,
so it would land a graph that looks like a small organization rather than a failed
collect.

A variable set to the EMPTY STRING is present and empty, which a stdio child can
tell apart from absent. This collector treats it as a missing token, so an
operator reading "the variable is set" cannot conclude the collector has a
credential.

**No token value reaches the graph.** Secrets appear as names, scopes and
visibility and nothing else; a test asserts over the raw bytes of a whole collect
result that a planted credential appears in none of them.

## Collecting

```json
{"id": "acme", "params": {}}
```

The `id` is the GitHub organization to enumerate, and it is also the graph the
results land in. This collector reads no environment variable to resolve an
organization, so a call without one is refused rather than walked against
something else.

Two optional parameters bound the two enumerations that read history rather than
current state:

| Parameter | Default | Maximum |
|---|---|---|
| `max_runs` | 10 | 100 |
| `max_deployments` | 20 | 100 |

Both are per repository. A value above the maximum is REFUSED naming the
parameter and the ceiling, rather than answered with 100 and reported as what you
asked for.

## Anything this collector did not see is reported as not seen

Every collect asserts whether it enumerated the whole organization, and that
assertion decides whether the server may treat what the collect did not carry as
deleted.

| Outcome | What happened | What you do |
|---|---|---|
| refused | the token may not read one repository's secrets, runners or environments | grant the scope or the membership |
| partial | the provider did not return one repository, one environment or one workflow's definition | usually nothing; it is the provider's and it passes |

Neither fails the collect. Every other repository is still walked and everything
they found is still returned, including whatever page a refused or partial read
had already collected. What changes is the mark on the collect.

WHY THE INCOMPLETE MARK MATTERS MORE THAN IT LOOKS. A COMPLETE collect lets the
receiving server treat everything the collect did not carry as gone. A collector
that reported an unreadable repository as an ordinary empty result would
therefore, on the next collect, delete that repository's whole inventory and
report success.

If the repository listing itself fails, the collect fails outright: six of the
seven enumerations start from that list, so a walk that could not read it never
found out what the organization contains.

## Two collects of an unchanged organization produce the same result

The output is sorted by node id and by edge, and one node is emitted per id, so a
second collect of an organization nothing changed is identical to the first, byte
for byte. A diff between two collects shows what moved in your CI/CD and nothing
else.

## Running its tests

```
cd cmd/collectors/github-actions
go test ./...
```

No credential and no network. Every converter runs against a recorded API
response, assembled into one organization so a single walk reaches the whole
declared vocabulary, and the suite asserts that the collector emits every resource
kind and every relationship it declares.
