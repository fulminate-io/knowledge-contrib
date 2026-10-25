# hubspot collector

A knowledge collector for HubSpot. It serves one tool over MCP on stdio: a
daemon spawns it, calls that tool with a collect id and the targets the collect
names, and writes what it returns into a graph of its own.

It reads. Every call it makes is a HubSpot CRM read (batch reads and searches
are POSTs that read), with your own HubSpot token.

## Install

```bash
export HUBSPOT_ACCESS_TOKEN=pat-...
curl -fsSL https://raw.githubusercontent.com/fulminate-io/knowledge-contrib/main/install.sh | sh -s -- hubspot
```

The install script copies `HUBSPOT_ACCESS_TOKEN` from your shell into the
collector's credential file (`~/.knowledge/collectors.d/hubspot.json`) and
writes the `${HUBSPOT_ACCESS_TOKEN}` reference into the entry. The value is
never written into the entry itself.

## The token

`HUBSPOT_ACCESS_TOKEN` is the only variable the collector reads: a HubSpot
Service Key, or the access token of an existing legacy private app. HubSpot no
longer lets new legacy private apps be created, so a new token is a Service Key,
created by a super admin or a user with the Developer tools access permission.
Grant the read scopes for what you collect:

| Read | Scope |
|---|---|
| contacts | `crm.objects.contacts.read` |
| companies | `crm.objects.companies.read` |
| deals | `crm.objects.deals.read` |
| tickets | `tickets` |
| lists | `crm.lists.read` |

Calls, emails, meetings, notes and tasks are read as the records associated
with the ones you name; grant the scopes HubSpot asks for them. A rejected
token (401) or a missing scope (403) fails the collect and names the cause.

## Params

A collect names at least one target. A collect that names none is refused.

| Param | Type | What it selects |
|---|---|---|
| `companies`, `contacts`, `deals`, `tickets` | arrays of record ids | each record |
| `lists` | array of list ids | each member of each list |
| `searches` | array of `{"object_type": "contacts", "filter_groups": [...]}` | each hit of a CRM search of companies, contacts, deals or tickets; `filter_groups` is HubSpot's `filterGroups`, sent as written |
| `properties` | object of object type to property names, optional | extra properties to read, such as `{"contacts": ["jobtitle"]}`; absent reads HubSpot's default properties |

Every record a collect selects is read with the records associated with it,
one hop: its contacts, companies, deals, tickets, calls, emails, meetings,
notes and tasks. The associated records' own associations are not read.

A search is refused before any request when it has no filter, more than five
filter groups, more than six filters in a group, more than 18 filters in all,
or a body over 3,000 characters, HubSpot's own limits. A search with more than
HubSpot's 10,000 results per query is read on in `hs_object_id` windows.

```jsonc
collect({ "type": "hubspot", "id": "acme", "params": {
  "companies": ["9876543210"]
}})
```

A 429 is waited out the way HubSpot signals it: the
`X-HubSpot-RateLimit-Interval-Milliseconds` window for the per-app limit, one
second for a search; searches are sent at no more than five a second, which is
HubSpot's whole per-account search budget, so another integration searching the
same account makes these searches wait and can end them rate-limited. A read
still limited after three requests or twenty seconds of waiting, a spent daily
limit, an account migration (477) and a 429 HubSpot gives no wait for are kept
as far as they got and the walk is reported incomplete. `walk_complete` is also
false when an associated record does not read, an associations batch answers
with errors for some inputs, a server error interrupts a read, or the walk is
cancelled.

## What it produces

| Node type | What it is |
|---|---|
| `hubspot-contact`, `hubspot-company`, `hubspot-deal`, `hubspot-ticket` | a CRM record |
| `hubspot-call`, `hubspot-email`, `hubspot-meeting`, `hubspot-note`, `hubspot-task` | an engagement; its body is the node's `content` |
| `hubspot-list` | a list the collect named |

A record carries `record_id`, `object_type`, `created_at`, `updated_at`,
`archived` and `property:<name>` for every property it was read with; a list
carries `list_id` and `object_type_id`.

| Edge type | From | To |
|---|---|---|
| `ASSOCIATED_WITH` | a record | a record associated with it; the edge's `evidence` is the association types as JSON (`category`, `typeId`, `label`) |
| `HAS_MEMBER` | a list | each of its members |

Node ids are `hubspot:<object type>:<record id>` (a contact and a company with
the same record id are two nodes) and `hubspot:list:<list id>`.

## Register by hand

```bash
knowledge collector add --tool collect -e 'HUBSPOT_ACCESS_TOKEN=${HUBSPOT_ACCESS_TOKEN}' hubspot -- /absolute/path/to/knowledge-collector-hubspot
```

with `HUBSPOT_ACCESS_TOKEN` set in `collectors.d/hubspot.json` beside your
`collectors.json`.
