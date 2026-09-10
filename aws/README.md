# aws collector

Enumerates an AWS account's resources and the relationships among them, and
returns them as the knowledge collector contract's nodes and edges. It is an MCP
server: the knowledge daemon spawns it, calls one tool, and writes the result
into a graph family named by the config entry that installed it.

It reads. It creates nothing, changes nothing and deletes nothing in the account,
and it never reads a secret's value.

## What it produces

One node type, `cloud-resource`. What KIND of resource a node is rides in its
`resource_type` metadata, which is what a query selects on: 53 values, from
`ec2-instance` and `s3-bucket` through `iam-role` to `apigw:httpapi`.

**One resource type is not collected: `ses-receipt-rule`.** The AWS SDK v2 `sesv2`
package this collector is built on carries no receipt-rule operation at all;
receipt rules are only reachable through the v1 `ses` API, which this module does
not depend on. So SES identities are collected and SES receipt rules are not. The
gap is declared in the source rather than left as a discrepancy, and a test reds
if a future SDK gains the operation.

A node's id is the resource's real ARN wherever AWS returns one. Two id forms
are composed, because the API returns no ARN for them:

- `aws:cidr:<cidr>` — a sentinel standing for a CIDR block a security-group or
  network-ACL rule references, so both ends of an ALLOWS edge are nodes.
- `aws:dynamodb:pitr/<table>` — point-in-time recovery, which is a table setting
  rather than an API object.

33 edge relationships, among them `USES_SECURITY_GROUP`, `ASSUMES_ROLE`,
`ENCRYPTS_WITH`, `ALLOWS_INGRESS_FROM`, `TRUSTS` and `WORKLOAD_IDENTITY`.

Some edges point at things this account does not contain, deliberately: a rule
referencing another account's security group, a role trusting another account's
principal, an IRSA binding naming a Kubernetes ServiceAccount. Those are real
relationships, and the far end resolves when the other graph is collected.

## Installing it

Released builds live in the public **knowledge-contrib** repository: every
release there carries one archive per collector per platform, named
`knowledge-collector-aws-<os>-<arch>.tar.gz` (`.zip` on Windows) and holding a
single binary called `knowledge-collector-aws`, plus a `checksums.txt` covering
all of them. The install script in that repository is the way to get one: it
picks the archive for your platform, verifies it against `checksums.txt` and
installs nothing if the checksum is missing or does not match. Building from
source stays supported and is described below.

The one command is:

```bash
curl -fsSL https://raw.githubusercontent.com/fulminate-io/knowledge-contrib/main/install.sh | sh -s -- aws
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
curl -fsSL https://raw.githubusercontent.com/fulminate-io/knowledge-contrib/main/uninstall.sh | sh -s -- aws
```

Or write the registration yourself.

Registration is a config file. Install or build the binary, then write an entry with
`knowledge collector add`, or by hand into `~/.knowledge/collectors.json` (or a
repository's own `.knowledge/collectors.json`).

**The entry name is the graph family** the results land in, and the collect id is
the instance — conventionally the AWS account id.

```json
{
  "collectors": {
    "aws": {
      "type": "stdio",
      "command": "/home/knowledge/.knowledge/bin/knowledge-collector-aws",
      "tool": "collect_aws",
      "env": {
        "AWS_PROFILE": "production",
        "AWS_REGION": "us-east-1",
        "HOME": "/home/knowledge"
      }
    }
  }
}
```

**No `behavior` block is needed to search the graph.** An entry that declares
none is keyword-searchable from its first collect, and it is neither summarized
nor embedded: both of those are LLM calls, so they are opt-in per collector.

Add them if you want semantic search over this graph, either on the command line
or by editing the entry:

```
knowledge collector add --tool collect_aws --embeddable=true aws -- /home/knowledge/.knowledge/bin/knowledge-collector-aws
```

`--summarizable=true` opts into LLM summaries the same way. Nodes this collector
already summarized are never re-summarized, so opting in fills only the gaps.

The field lists (`--embed-fields`, `--summarize-fields`, `--bm25-fields`, each
repeatable) choose which node fields the text is composed from. Leave them out
and the default is `symbol_name`, `summary`, `keywords`, `description` and
`content`. The two this collector fills are `summary`, the one-line description
of each resource, and `content`, its detail.

Then collect:

```
collect(type: "aws", id: "123456789012")
collect(type: "aws", id: "123456789012", params: {"region": "eu-west-1"})
```

`params` is optional. `region` selects which region to walk; `account` is a guard
that refuses the collect if the credentials resolve to a different account, since
this collector assumes no role.

## The environment

**The `env` block is this process's whole environment.** The daemon copies
nothing from its own and adds nothing, so a variable the block does not name is
absent here — however it is set on the host.

**Set only what you are setting.** A name present with an empty value is not the
same as an absent one: `"AWS_PROFILE": ""` asks the SDK for a profile whose name
is the empty string, which resolves nothing, where omitting it asks for the
default profile.

`HOME` is not optional if you use a profile, an SSO session, or a role from the
shared files: the SDK resolves `~/.aws/config` and `~/.aws/credentials` through
it. On Windows the equivalent is `USERPROFILE`. If neither is set, no profile is
reachable whatever else the block carries.

Everything below is a name this collector's dependency closure reads. A test in
this module censuses the pinned AWS SDK's own source and fails if this list and
the SDK's read set disagree in either direction.

### Credentials, region and SDK behaviour

The AWS SDK's configuration surface: static credentials, the profile and shared
files, web identity and container credentials, the instance metadata service,
retry and checksum behaviour, dualstack and FIPS endpoints, and the TLS bundle.

- `AWS_ACCESS_KEY`
- `AWS_ACCESS_KEY_ID`
- `AWS_ACCOUNT_ID`
- `AWS_ACCOUNT_ID_ENDPOINT_MODE`
- `AWS_AUTH_SCHEME_PREFERENCE`
- `AWS_CA_BUNDLE`
- `AWS_CONFIG_FILE`
- `AWS_CONTAINER_AUTHORIZATION_TOKEN`
- `AWS_CONTAINER_AUTHORIZATION_TOKEN_FILE`
- `AWS_CONTAINER_CREDENTIALS_FULL_URI`
- `AWS_CONTAINER_CREDENTIALS_RELATIVE_URI`
- `AWS_DEFAULTS_MODE`
- `AWS_DEFAULT_PROFILE`
- `AWS_DEFAULT_REGION`
- `AWS_DISABLE_REQUEST_COMPRESSION`
- `AWS_EC2_METADATA_DISABLED`
- `AWS_EC2_METADATA_SERVICE_ENDPOINT`
- `AWS_EC2_METADATA_SERVICE_ENDPOINT_MODE`
- `AWS_EC2_METADATA_V1_DISABLED`
- `AWS_ENABLE_ENDPOINT_DISCOVERY`
- `AWS_ENDPOINT_URL`
- `AWS_EXECUTION_ENV`
- `AWS_IGNORE_CONFIGURED_ENDPOINT_URLS`
- `AWS_LOGIN_CACHE_DIRECTORY`
- `AWS_MAX_ATTEMPTS`
- `AWS_PROFILE`
- `AWS_REGION`
- `AWS_REQUEST_CHECKSUM_CALCULATION`
- `AWS_REQUEST_MIN_COMPRESSION_SIZE_BYTES`
- `AWS_RESPONSE_CHECKSUM_VALIDATION`
- `AWS_RETRY_MODE`
- `AWS_ROLE_ARN`
- `AWS_ROLE_SESSION_NAME`
- `AWS_S3_DISABLE_EXPRESS_SESSION_AUTH`
- `AWS_S3_DISABLE_MULTIREGION_ACCESS_POINTS`
- `AWS_S3_USE_ARN_REGION`
- `AWS_SDK_UA_APP_ID`
- `AWS_SECRET_ACCESS_KEY`
- `AWS_SECRET_KEY`
- `AWS_SESSION_TOKEN`
- `AWS_SHARED_CREDENTIALS_FILE`
- `AWS_USE_DUALSTACK_ENDPOINT`
- `AWS_USE_FIPS_ENDPOINT`
- `AWS_WEB_IDENTITY_TOKEN_FILE`

### Per-service endpoint overrides

One per AWS service this collector builds a client for, for private-endpoint,
LocalStack-style and interception deployments. Four of them — STS, SSO, SSO OIDC
and Signin — belong to clients the credential chain builds on your behalf and
that this collector never calls directly; they are here because pointing the
account's authentication at a private endpoint goes through exactly those.

- `AWS_ENDPOINT_URL_ACM`
- `AWS_ENDPOINT_URL_APIGATEWAYV2`
- `AWS_ENDPOINT_URL_API_GATEWAY`
- `AWS_ENDPOINT_URL_CLOUDFRONT`
- `AWS_ENDPOINT_URL_CLOUDTRAIL`
- `AWS_ENDPOINT_URL_CLOUDWATCH`
- `AWS_ENDPOINT_URL_CLOUDWATCH_LOGS`
- `AWS_ENDPOINT_URL_DYNAMODB`
- `AWS_ENDPOINT_URL_EC2`
- `AWS_ENDPOINT_URL_ECR`
- `AWS_ENDPOINT_URL_ECS`
- `AWS_ENDPOINT_URL_EFS`
- `AWS_ENDPOINT_URL_EKS`
- `AWS_ENDPOINT_URL_ELASTICACHE`
- `AWS_ENDPOINT_URL_ELASTIC_LOAD_BALANCING_V2`
- `AWS_ENDPOINT_URL_EVENTBRIDGE`
- `AWS_ENDPOINT_URL_IAM`
- `AWS_ENDPOINT_URL_KINESIS`
- `AWS_ENDPOINT_URL_KMS`
- `AWS_ENDPOINT_URL_LAMBDA`
- `AWS_ENDPOINT_URL_OPENSEARCH`
- `AWS_ENDPOINT_URL_RDS`
- `AWS_ENDPOINT_URL_REDSHIFT`
- `AWS_ENDPOINT_URL_ROUTE_53`
- `AWS_ENDPOINT_URL_S3`
- `AWS_ENDPOINT_URL_SECRETS_MANAGER`
- `AWS_ENDPOINT_URL_SESV2`
- `AWS_ENDPOINT_URL_SFN`
- `AWS_ENDPOINT_URL_SIGNIN`
- `AWS_ENDPOINT_URL_SNS`
- `AWS_ENDPOINT_URL_SQS`
- `AWS_ENDPOINT_URL_SSO`
- `AWS_ENDPOINT_URL_SSO_OIDC`
- `AWS_ENDPOINT_URL_STS`

### Read by the Go standard library

No census of the AWS SDK surfaces these, because the standard library is outside
every module's dependency closure. The proxy family is six names because
`net/http` reads both cases, and the AWS SDK's default HTTP client uses it. The
two trust-root names matter because `AWS_CA_BUNDLE` above overrides the SDK's own
bundle and does not reach the standard library's system pool, so setting one
without the other leaves half a trust story.

- `HOME`
- `HTTPS_PROXY`
- `HTTP_PROXY`
- `NO_PROXY`
- `SSL_CERT_DIR`
- `SSL_CERT_FILE`
- `USERPROFILE`
- `http_proxy`
- `https_proxy`
- `no_proxy`

## Failure

A missing or unusable credential fails the collect, naming every source the
default chain tried. Nothing is written.

A single service failing — one denied permission, one throttle that outlasts the
SDK's own retries — does NOT fail the collect. The result ships with what was
enumerated and `walk_complete` false, which is what stops the server treating
everything the failed service would have named as deleted. The failed services
are named in the walk's own message.

The same holds one level down. Several resource types need a second, per-resource
call: a load balancer's listeners, a domain's base-path mappings, an event rule's
targets, a task definition's images, a table's backup setting, a certificate's
validation. If one of those is denied while the service itself lists fine, the
walk keeps everything it read AND still reports `walk_complete` false, naming the
operation. That matters more than it sounds: your account's graph is replaced
wholesale on every complete collect, so a read that failed quietly would not just
be missing for one collect, it would DELETE what the previous collect found.

The practical consequence for an IAM policy: a narrower policy costs you edges and
marks collects incomplete, rather than silently shrinking your graph.

An empty account is a complete walk, not a failed one.

## What it declares about itself

This collector serves the contract's required `describe` tool: one document
naming its suggested behavior and field lists, its one node type and its thirty-three edge types, the environment
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
knowledge-collector-aws --describe-tool        # the tool name a config entry's `tool` field takes
knowledge-collector-aws --describe-env-table   # one `class name` row per declared variable
```

## Building

```
cd cmd/collectors/aws && go build -o knowledge-collector-aws .
```
