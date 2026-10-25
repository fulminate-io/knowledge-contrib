# notion collector

A knowledge collector for Notion. It serves one tool over MCP on stdio: a daemon
spawns it, calls that tool with a collect id and the targets the collect names,
and writes what it returns into a graph of its own.

It reads. Every call it makes is a Notion API read (a data source query and a
search are POSTs that read), with your own Notion token.

## Install

```bash
export NOTION_API_KEY=ntn_...
curl -fsSL https://raw.githubusercontent.com/fulminate-io/knowledge-contrib/main/install.sh | sh -s -- notion
```

The install script copies `NOTION_API_KEY` from your shell into the
collector's credential file (`~/.knowledge/collectors.d/notion.json`) and writes
the `${NOTION_API_KEY}` reference into the entry. The value is never written
into the entry itself.

## The token

`NOTION_API_KEY` is the only variable the collector reads: a personal access
token, which acts as you and reads every page you can see, or an internal
connection token, which reads the pages shared with that connection. A personal
access token expires (7 days to 1 year) and has no refresh; a collect with an
expired, revoked or missing token fails and says so, and the fix is a new
token.

## Params

A collect names at least one target. A collect that names none is refused.

| Param | Type | What it selects |
|---|---|---|
| `pages` | array of page ids | each page: every block, recursively, and every unresolved comment on the page and its blocks |
| `descendants` | boolean, optional | also read every child page and child database of every page read, recursively |
| `databases` | array of database ids | every entry of every data source of each database |
| `data_sources` | array of `{"id": "<data source id>", "filter": {...}}` | every entry of the data source the optional Notion filter object selects; the filter is sent as written and may filter on properties, `created_time` or `last_edited_time` |
| `title_search` | array of query strings | every page and data source whose title matches; pages are read, data sources queried |

```jsonc
collect({ "type": "notion", "id": "acme", "params": {
  "pages": ["59833787-2cf9-4fdf-8782-e53db20768a5"],
  "descendants": true
}})
```

Each collect walks its targets completely: every page of every list, every
level of every block tree. A 429 or 529 is retried after the `Retry-After` Notion
names, up to three requests per read and two minutes of waiting in total; a read
still limited after that is kept as far as it got and the walk is reported
incomplete with the path named. A data source query Notion caps at its result
limit is read on in `created_time` windows. `walk_complete` is false whenever a
read did not return its whole scope: a capped query no window gets past, a
server error mid-read, a child page the token cannot read, or a cancelled walk.

Resolved comments are not returned by Notion's API, so they are not collected.
A block type the API does not support (a form, a button) is listed on its page
in `unsupported_blocks`. A file Notion hosts reaches the graph by its caption or
name only, because its URL is a signed link that expires within an hour; an
external file keeps its URL.

## What it produces

| Node type | What it is | Metadata keys |
|---|---|---|
| `notion-page` | a page or a database entry; its block text, in document order, is the node's `content` | `page_id`, `url`, `created_time`, `last_edited_time`, `created_by`, `parent_type`, `parent_id`, `in_trash`, `unsupported_blocks`, and `property:<name>` for every property |
| `notion-database` | a database the collect read | `database_id`, `url`, `created_time`, `parent_type`, `parent_id` |
| `notion-data-source` | a data source of a database | `data_source_id`, `database_id` |
| `notion-comment` | an unresolved comment; its text is the node's `content` | `comment_id`, `page_id`, `block_id`, `discussion_id`, `created_time`, `created_by` |

| Edge type | From | To |
|---|---|---|
| `CHILD_OF` | a child page or child database | the page it sits in |
| `BELONGS_TO` | a data source, or a database entry | its database, or its data source |
| `COMMENTS_ON` | a comment | the page it, or its block, sits in |

Node ids are `notion:page:<id>`, `notion:database:<id>`,
`notion:data-source:<id>` and `notion:comment:<id>`.

## Register by hand

```bash
knowledge collector add --tool collect -e 'NOTION_API_KEY=${NOTION_API_KEY}' notion -- /absolute/path/to/knowledge-collector-notion
```

with `NOTION_API_KEY` set in `collectors.d/notion.json` beside your
`collectors.json`.
