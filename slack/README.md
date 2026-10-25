# slack collector

A knowledge collector for Slack. It serves one tool over MCP on stdio: a daemon
spawns it, calls that tool with a collect id and the targets the collect names,
and writes what it returns into a graph of its own.

It reads. Every call it makes is a Slack Web API read method, with your own
Slack token.

## Install

```bash
export SLACK_TOKEN=xoxb-...   # or a user token, xoxp-...
curl -fsSL https://raw.githubusercontent.com/fulminate-io/knowledge-contrib/main/install.sh | sh -s -- slack
```

The install script copies `SLACK_TOKEN` from your shell into the collector's
credential file (`~/.knowledge/collectors.d/slack.json`) and writes the
`${SLACK_TOKEN}` reference into the entry. The value is never written into the
entry itself.

## The token

`SLACK_TOKEN` is the only variable the collector reads. A collect with no token,
or with one Slack refuses (`invalid_auth`, `not_authed`, `token_revoked`,
`token_expired`, `account_inactive`), fails and names the cause.

Create a Slack app in your workspace and grant the scopes for the targets you
collect:

| Read | Scopes |
|---|---|
| channel history and thread replies | `channels:history`, `groups:history`, `im:history`, `mpim:history` (the ones for the conversation kinds you name) |
| channel names and topics | `channels:read`, `groups:read`, `im:read`, `mpim:read` |
| the users who posted | `users:read` |
| `params.search` | `search:read`, on a **user** token (`xoxp`) |

A bot token reads only the channels the bot is a member of; a user token reads
the public channels and the private ones its user belongs to. A missing scope
fails the collect, naming the method and the scopes it takes.

## Params

A collect names at least one target. A collect that names none is refused.

| Param | Type | What it selects |
|---|---|---|
| `channels` | array of conversation ids | every message of each channel, and every reply of every thread in it |
| `threads` | array of `{"channel": "<id>", "ts": "<parent ts>"}` | one thread each: its parent and every reply |
| `search` | array of query strings | every `search.messages` match of each query; Slack's own `in:`, `from:`, `before:`, `after:` modifiers work inside a query |
| `start_time` | RFC 3339 timestamp, optional | the window's start, inclusive, for every target |
| `end_time` | RFC 3339 timestamp, optional | the window's end, inclusive, for every target |

```jsonc
collect({ "type": "slack", "id": "acme", "params": {
  "channels": ["C0123456789"],
  "start_time": "2026-01-01T00:00:00Z"
}})
```

Each collect walks its targets completely: every history page, every reply page
and every search page. A 429 is retried after the `Retry-After` Slack names, up
to three requests per read and two minutes of waiting in total; a read still
rate-limited after that is kept as far as it got and the walk is reported
incomplete with the method named. `walk_complete` is false whenever a read did
not return its whole scope: a free-plan workspace whose history is cut off
(`is_limited`), a search whose reported total exceeds the matches read, a
server error mid-read, or a cancelled walk.

## What it produces

| Node type | What it is | Metadata keys |
|---|---|---|
| `slack-channel` | a conversation the collect read | `channel_id`, `name`, `kind` (`channel`, `group`, `im`, `mpim`), `is_private`, `is_archived`, `topic`, `purpose`, `created` |
| `slack-message` | a message or a thread reply; the text is the node's `content` | `channel_id`, `ts`, `user`, `bot_id`, `username`, `subtype`, `thread_ts`, `reply_count`, `permalink` |
| `slack-user` | a user who posted a message the collect read | `user_id`, `team_id`, `name`, `real_name`, `display_name`, `title`, `tz`, `is_bot`, `deleted` |

| Edge type | From | To |
|---|---|---|
| `BELONGS_TO` | a message | its channel |
| `POSTED_BY` | a message | the user who posted it (a bot message, which names no user, has none) |
| `REPLIES_TO` | a thread reply | the thread's parent message |

Node ids are `slack:channel:<channel id>`, `slack:message:<channel id>:<ts>` and
`slack:user:<user id>`.

## Register by hand

```bash
knowledge collector add --tool collect -e 'SLACK_TOKEN=${SLACK_TOKEN}' slack -- /absolute/path/to/knowledge-collector-slack
```

with `SLACK_TOKEN` set in `collectors.d/slack.json` beside your `collectors.json`.
