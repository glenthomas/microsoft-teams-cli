# microsoft-teams-cli

A command line interface for Microsoft Teams.

`teams` reads Teams data (teams, channels, messages and threads) through
Microsoft Graph. It is built to be called by **AI coding agents** as a
lightweight alternative to an MCP server: every command writes structured JSON
to stdout, errors are JSON on stderr, and exit codes are stable.

```sh
teams search \
  --channel platform-engineering \
  --since 30d \
  --query "private endpoints"
```

## Install

Requires Go 1.24+.

```sh
go install github.com/glenthomas/microsoft-teams-cli/cmd/teams@latest
# or, from a clone:
go build -o teams ./cmd/teams
```

## Authenticate

```sh
teams login
```

A browser window opens so you can sign in with your work or school account
(OAuth 2.0 authorization code flow with PKCE; the CLI listens on a temporary
`http://localhost` port for the redirect). If a browser cannot be opened the
sign-in URL is printed to stderr. On headless machines use:

```sh
teams login --device-code
```

Tokens are cached (owner-only file permissions) in the CLI config directory
(`~/.config/teams-cli` on Linux, `~/Library/Application Support/teams-cli` on
macOS, `%AppData%\teams-cli` on Windows, or `$TEAMS_CLI_CONFIG_DIR`) and are
refreshed silently by later commands. `teams logout` removes them.

### Permissions and app registration

The CLI requests these delegated Microsoft Graph permissions:
`User.Read`, `Team.ReadBasic.All`, `Channel.ReadBasic.All`,
`ChannelMessage.Read.All`, `ChannelMessage.Send`, `Chat.ReadBasic`, `Chat.Read`,
and `ChatMessage.Send` (plus `offline_access`).
`ChannelMessage.Read.All` requires **admin consent** in most tenants.
`ChannelMessage.Send` is used by `teams post`; admin consent is not generally
required, though tenant policies can restrict user consent.
The chat permissions are used by `teams chats`, `teams chat-messages`, and
`teams chat-post`. Their delegated Graph permissions do not generally require
admin consent, though tenant consent policies can still require approval.

By default the public *Microsoft Graph Command Line Tools* application
(`14d82eec-204b-4c2f-b7e8-296a70dab67e`) and the `organizations` authority are
used. To use your own app registration (a public client with the
`http://localhost` redirect URI under "Mobile and desktop applications") or a
specific tenant:

```sh
teams login --client-id <app-id> --tenant contoso.onmicrosoft.com
```

The client ID and tenant used at login are remembered for later commands.
They can also be set with `TEAMS_CLI_CLIENT_ID` / `TEAMS_CLI_TENANT_ID`.

If you already have a Graph access token (e.g. in CI), set
`TEAMS_CLI_ACCESS_TOKEN` and no login is needed.

## Commands

| Command | Description |
| --- | --- |
| `teams login [--device-code] [--login-hint user@x] [--timeout 5m]` | Sign in (opens a browser) |
| `teams logout` | Remove cached credentials |
| `teams whoami` | Show the signed-in user |
| `teams teams` | List teams you are a member of |
| `teams channels [--team T]` | List channels in a team, or in all your teams |
| `teams chats` | List your one-to-one and group chats (excludes meeting chats) |
| `teams messages --channel C [--team T] [--since 7d]` | List recent messages and replies, newest first |
| `teams search --channel C [--team T] --query Q [--since 30d]` | Search messages and replies, newest first |
| `teams thread --channel C [--team T] --id ID` | Show a thread (root + replies), oldest first |
| `teams post --channel C [--team T] --message TEXT [--reply-to ID]` | Post a channel message or reply to a thread |
| `teams chat-messages --chat ID` | List messages in a one-to-one or group chat, newest first |
| `teams chat-post --chat ID --message TEXT` | Send a message to a one-to-one or group chat |

`--reply-to` takes the root message ID (the `threadId` field from `messages`,
`search`, or `thread` output). For example:

```sh
teams post --channel platform-engineering --message "Deployment is complete"
teams post --channel platform-engineering --reply-to 1717171717171 --message "Acknowledged"
teams chats
teams chat-messages --chat '19:abc@thread.v2'
teams chat-post --chat '19:abc@thread.v2' --message "I will take a look"
```

Use the chat ID from `teams chats` with `chat-messages` and `chat-post`. Chat
history is limited to chats the signed-in user participates in.

Common flags for `messages` and `search`:

- `--channel/-c` channel name or ID (`19:...@thread.tacv2`). Names match
  case-insensitively and ignore spaces/punctuation, so `platform-engineering`
  matches *Platform Engineering*. If a name exists in several teams, add
  `--team`.
- `--team/-t` team name or ID. Without `--channel`, every channel in the team
  is scanned.
- `--since/-s`, `--until` relative (`90m`, `12h`, `30d`, `2w`, `3mo`, `1y`),
  a date (`2026-09-01`) or an RFC 3339 timestamp.
- `--query/-q` (search) all words must appear (case-insensitive); use double
  quotes for exact phrases: `--query '"private endpoint" dns'`.
- `--limit/-n` maximum results (default 50, `0` = unlimited).
- `--no-replies` only root messages.
- `--max-threads` maximum threads scanned per channel (default 1000).

Global flags: `--format json|text` (default `json`), `--client-id`, `--tenant`.

## Output

`search` / `messages` return:

```json
{
  "query": "private endpoints",
  "terms": ["private", "endpoints"],
  "since": "2026-08-31T15:00:00Z",
  "channels": [
    {"teamId": "…", "teamName": "Platform", "channelId": "19:…@thread.tacv2", "channelName": "Platform Engineering"}
  ],
  "count": 1,
  "truncated": false,
  "messages": [
    {
      "id": "1727000000000",
      "type": "message",
      "threadId": "1727000000000",
      "teamId": "…",
      "teamName": "Platform",
      "channelId": "19:…@thread.tacv2",
      "channelName": "Platform Engineering",
      "author": "Alice Smith",
      "authorId": "…",
      "createdDateTime": "2026-09-27T10:12:00Z",
      "text": "Should we use private endpoints for ACR?",
      "webUrl": "https://teams.microsoft.com/l/message/…",
      "replyCount": 2
    }
  ]
}
```

`type` is `message` for a thread's root post or `reply`; pass `threadId` to
`teams thread --id` to read the full conversation. `truncated` is `true` when
`--limit` or `--max-threads` cut the results short (details in `warnings`).
Message bodies are converted from HTML to plain text.

Errors are written to stderr:

```json
{"error": {"code": "ambiguous", "message": "channel \"general\" is ambiguous; use --team to disambiguate. …", "details": {…}}}
```

| Exit code | `error.code` | Meaning |
| --- | --- | --- |
| 0 | | Success |
| 1 | `error`, `timeout` | Unexpected error, or `teams login` exceeded `--timeout` |
| 2 | `usage` | Invalid flags or arguments |
| 3 | `not_logged_in` | No cached sign-in or it expired — run `teams login` |
| 4 | `not_found`, `ambiguous` | Team/channel/message not found, or name matched several |
| 5 | `forbidden`, `throttled`, `graph_error` | Microsoft Graph API error |

## How search works

Microsoft Graph has no server-side text filter for channel messages, so the
CLI pages through the channel's threads (newest activity first, with replies
expanded) and filters them locally by time and text. Paging stops at the first
page with no activity after `--since`, so a tight `--since` keeps searches
fast. Throttled requests (HTTP 429/503) are retried honouring `Retry-After`.

## Development

```sh
go build ./...
go vet ./...
go test ./...
```
