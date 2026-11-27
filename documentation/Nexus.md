# Nexus

The **Nexus library app** (forge 26d-3, [forge plan](../../forge/documentation/plans/026d-3-NexusLibrary.md)) is native channels' data and logic: entity types, roles and row rules, command and query graphs, the unread projector, two triggers and the tools agents use. It has no pages. Forge's Messaging app (26d-4) draws it, and the bridges (26f-2) and agents in channels (26j) build on it.

## Where it lives

`backend/features/nexus`. The app is the files under `app/`, laid out as a Forge sample: `app.json`, `entities/` and `graphs/`. The graph JSON is the source of truth. Forge's `forge graph spec`/`forge graph gen` and `forge graph lint` edit and check it.
- `nexus.Files()` gives the files to Forge's starter, which seeds Nexus with the other library apps (`starter.LoadApps`, `forge seed`).
- `nexus.Document()` is the app as an export document. It passes `apps.CheckDocument`.
- `nexus.Seed(args, store, appservice, bus)` imports and installs it in a tenant once, as the service principal.

## Data

| Entity type | What it holds |
|---|---|
| `nexusChannel` | `name`, `topic`, `kind` (`channel` or `direct`), `private` (fixed once made), `archived`, `owner`, `peer` (a direct conversation's other person) |
| `nexusMember` | One person's membership, with id `<channel>-<user>`: `channel` (a relation), `user`, `role` (`admin` or `member`), `owner` (the channel's, copied), `private` (the channel's, copied), `seen` |
| `nexusMessage` | `channel`, `author`, `authorName` (for agents and outside authors), `text` (`markdown`, searchable), `parent` (a thread's first message), `mentions` (user ids), `files`, `private` (the channel's, copied), `edited`, `deleted`. Its id is the sender's browser's, so a message posted twice is one |
| `nexusReaction` | One person's reactions to one message, with id `<message>-<user>`: the emoji they reacted with |

Messages are kept forever in iteration 26 (question 13). The records fields they need copy the channel's privacy, so row rules need no lookup.

## Commands

All are graphs. Each command's plan reads what it needs, then refuses or writes:
- `nexus.channel.create`, `join` (not private, not archived), `leave`, `addMember` (a member adds people; never to a direct conversation), `archive` (its owner only), `markRead {count}`.
- `nexus.dm.open {user}`: its target is `dm-<lower id>-<higher id>`, and it makes the channel and both memberships once.
- `nexus.message.post` (members only, not in an archived channel; a second post with the same id changes nothing), `message.edit` and `message.delete` (by the author only; a deleted message keeps its place with no text).
- `nexus.reaction.toggle {emoji}`.

## Queries

- `nexus.channels` lists the caller's channels and direct conversations from their memberships.
- `nexus.unread {channel}` gives `{count, lastAt}` from the `unread` projector (`app.nexus.unread`, keyed by channel, counting messages that are not thread replies). A member's unread count is `count` less their `seen`. It gives nothing for a channel the caller cannot see.
- `nexus.messages {channel, parent?}` gives the latest messages. Pages show a channel with a reversed `mui.virtualList` over `nexusMessage` instead.

## Access

| Role | What it allows |
|---|---|
| *Nexus administrator* | The installer's role: everything |
| *Nexus user* | Granted to the tenant's people by an administrator: makes channels and direct conversations, joins and posts, and edits its own messages. Row rules hide a private channel and everything in it unless the person holds *Private channel member* on it, or owns it, or the record is their own membership |
| *Private channel member* | Granted on one private channel by the app's `onMember` trigger when someone joins it, and revoked when they leave |

The trigger grants the role only when whoever made the membership (`_createdBy`) is the channel's owner or already holds the role there. A membership an outsider forges for themselves therefore gives no access. Entity access rules keep a message's edits to its author, a membership's changes to its person, and a channel's changes to its owner.

The manifest declares one ephemeral topic, `typing`, keyed by a channel id and readable by whoever may see the channel (26d-1's *Publish* and `live.ephemeral`).

The `onMessage` trigger notifies the people a message mentions (`notify.send`, key `nexus:<message>:mention`). In a private channel it notifies only members, and never the author.

## Tools

The manifest gives four tools to chat and agents: `nexus.readChannel` and `nexus.search` (read), and `nexus.post` and `nexus.react` (write: asked in chat, decided by an agent's own roles).

## Changes it needed elsewhere

- **App queries reach an app's users.** An installed app's query whose spec says `record.read` used to be checked on the tenant, which no app role holds. It is now checked as the app's reach: `record.read` on any of the app's own entity types (`installs.reaches`). Its graph still reads only what row rules allow.
- **`notifications.Ensure` caches per projections**, so a test's fresh server is asked again.

## Tests

`go test ./features/nexus`:
- `TestChannels` covers posting, joining, retries, unread counts, mark read, editing, deleting, reactions, mentions and archiving.
- `TestPrivateChannels` covers hiding, adding, a forged membership, leaving and direct conversations.
- `TestDocument` checks the blueprints-only document.

The fixture is the stack serve wires, with access control and a job runner for the triggers.

## Not built yet

- Mention counts per channel: mentions reach people as notifications instead.
- Removing someone else from a channel.
- Retention settings and channel export.
- A record command sent directly (`record.create` on `nexusMessage`) skips the command's checks: entity and row rules still hold, but such a message may claim `private: false` in a private channel.
