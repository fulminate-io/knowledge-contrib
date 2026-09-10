# knowledge-contrib

The published home of the pre-built knowledge collectors, the framework they are
built on, and the script that installs them.

A knowledge collector is a process that speaks MCP and answers with nodes and
edges. The knowledge daemon spawns it, calls one tool, and writes what comes
back into a graph of its own. The collectors here are the ready-made ones: each
is a single static binary, published one archive per collector per platform on
every release, with a `checksums.txt` covering all of them.

## What is here

<!-- BEGIN GENERATED: modules -->
- [`aws`](aws/)
- [`azure`](azure/)
- [`bitbucket-pipelines`](bitbucket-pipelines/)
- [`cloudwatch`](cloudwatch/)
- [`gcp`](gcp/)
- [`github-actions`](github-actions/)
- [`gitlab-ci`](gitlab-ci/)
- [`k8s-logs`](k8s-logs/)
- [`k8s`](k8s/)
- [`loki`](loki/)
- [`stackdriver`](stackdriver/)
<!-- END GENERATED: modules -->

Each module carries its own README: what it produces, what it reads, how it is
built, and the worked config entry that registers it.

`framework/` is the Go framework the collectors are built on, and the place to
start if you are writing your own. Its README is the contract as well: what a
collector process must speak, in any language.

## Installing one

```bash
curl -fsSL https://raw.githubusercontent.com/fulminate-io/knowledge-contrib/main/install.sh | sh -s -- <collector>
```

It detects your platform, downloads that collector's archive and the checksums,
verifies the archive against them, places the binary under `.knowledge/bin` in
your home directory, records the release tag in a sidecar beside it, and
registers the collector with `knowledge collector add`. It installs nothing at
all when the checksum is missing or does not match. `--version <tag>` pins a
release; `--print-env-table` prints the per-collector environment table and
exits; `--print-env-sensitive-table` prints, per collector, the environment names
that collector tells present-and-empty apart from absent, and exits. Both tables
are derived from the collectors' own declarations rather than written here.

Reversing it removes the binary, its version sidecar and the config entry, and
no graph:

```bash
curl -fsSL https://raw.githubusercontent.com/fulminate-io/knowledge-contrib/main/uninstall.sh | sh -s -- <collector>
```

Neither script is required. A collector is registered by a config file entry and
nothing else, so building from source and writing the entry yourself is equally
supported; each module's README shows the entry.

**No provider credential is written into the config entry** by these scripts,
neither a value nor a reference to one, because a reference is expanded by the
process serving the collect and reaches the collector as an empty value that
looks set; a secret is supplied to the daemon's own environment instead.

## Writing your own

The framework is a Go module you import:

```go
import "github.com/fulminate-io/knowledge-contrib/framework"
```

You implement a walk and a params type; it serves the MCP tools, advertises the
contract schemas, validates the call and encodes the result. `framework/README.md`
carries the wire contract, a worked collector and the registration entry.

Nothing about the collectors published here is privileged. A collector you write
is dialed by the same client code, registered the same way, and refused on the
same terms.

## Releases

Each release is cut on the same tag as the knowledge client release, so a client
version and a collector version pair by tag. The archives are named
`knowledge-collector-<collector>-<os>-<arch>.tar.gz`, or `.zip` on Windows, and
each holds a single binary called `knowledge-collector-<collector>`.

## License

Apache-2.0. See `LICENSE`.
