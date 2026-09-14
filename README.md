# trello-mcp

## SABIZ maintained fork

This fork adds the complete official REST endpoint catalog (261 operations), generic read/write tools with JSON/form/multipart support, paginated comments/movement history, board member lookup, template copying, archived-card discovery and positioned moves. Version: `0.3.0-sabiz.2`.

**Install this fork from source using [docs/SETUP.md](docs/SETUP.md).** The npm/npx commands below are retained as upstream documentation and install the original package, not this fork. Existing board workflows stay scoped to their configured allowlist. Full-account API access requires a separate explicitly configured `TRELLO_API_SCOPE=account` entry; see the setup guide for boundaries and verification limits.

Before removing obsolete packages/files, present the exact cleanup list and obtain the user's confirmation. Keep commands and results documented; never include credentials or borrower data in Git.

## Upstream documentation

Universal [Model Context Protocol](https://modelcontextprotocol.io) server for Trello. Works with any MCP-compatible client: Cursor, Claude Desktop, Gemini CLI, Codex, OpenCode, and others.

The upstream tools read and update Kanban cards on allowlisted boards. The fork's additional account scope is described above. Credentials are supplied locally through environment variables.

## Quick start

### First time (recommended)

1. Install the package
2. Configure only `TRELLO_API_KEY` and `TRELLO_TOKEN` in your MCP client
3. Start the MCP server and ask your agent to set up Trello
4. The agent lists your boards, you pick one, and the selection is saved locally

No need to hunt for board ids on first install.

### Already know your board id?

Set `TRELLO_ALLOWED_BOARD_IDS` in the MCP env and skip onboarding.

## Features

- Single self-contained **native binary** (Go) — no Node/Bun runtime needed at runtime
- Prebuilt per-platform binaries shipped via npm `optionalDependencies` (esbuild-style)
- `npx -y @thadeu/trello-mcp` works on any machine, any Node version (the launcher is trivial)
- stdio transport (MCP standard for local tools)
- Interactive **onboarding** when no boards are configured
- Board allowlist via env var or saved config file
- Tools: list boards/lists/cards, get/create/update/move cards, add comments, attachments

## Requirements

- **Runtime:** none beyond your MCP client. The published package ships a native Go
  binary per platform; the `npx`/`trello-mcp` launcher is plain CommonJS and runs on
  any Node version (even 14) — no fnm/nvm gymnastics.
- **Supported platforms:** macOS and Linux on x64 and arm64.
- **Development:** [Go](https://go.dev) 1.25+ and [Bun](https://bun.sh) 1.2+ (build scripts).
- Trello API key and token ([Power-Up admin](https://trello.com/power-ups/admin)).
- Recommended: dedicated Trello service account, not a personal user token.

## Environment variables

| Variable | Required | Description |
|----------|----------|-------------|
| `TRELLO_API_KEY` | yes | API key from Trello Power-Up admin |
| `TRELLO_TOKEN` | yes | User token with `read,write` scope |
| `TRELLO_ALLOWED_BOARD_IDS` | no | Comma-separated board ids. Optional — see [Onboarding](#onboarding) |
| `TRELLO_CONFIG_PATH` | no | Override config file path (default: `~/.config/trello-mcp/config.json`) |

Copy `.env.example` when developing locally. MCP clients pass these via `env` in server config.

## Install

### Option 1 — npm / npx (recommended)

```bash
npx -y @thadeu/trello-mcp
```

Or install globally:

```bash
npm install -g @thadeu/trello-mcp@latest
```

npm automatically pulls the prebuilt binary for your platform
(`@thadeu/trello-mcp-<os>-<arch>`) through `optionalDependencies`.

### Option 2 — From source (Go)

```bash
git clone https://github.com/thadeu/trello-mcp.git
cd trello-mcp/cli
go build -o trello-mcp .
./trello-mcp            # runs the MCP server over stdio
```

Cross-compile every platform package (requires Go + Bun):

```bash
bun install
bun run build          # → npm/<platform>/bin/trello-mcp
```

## Onboarding

When `TRELLO_ALLOWED_BOARD_IDS` is **not** set and no config file exists yet, the server starts in **onboarding mode**:

- Setup tools work immediately
- Card/list tools are blocked until a board is selected
- After selection, all tools unlock **without restarting** the MCP server

### What gets saved

```json
// ~/.config/trello-mcp/config.json
{
  "allowedBoardIds": ["56c215abc7af5016b9bceadb"],
  "updatedAt": "2026-06-23T23:00:00.000Z"
}
```

### Config priority (on server start)

1. `TRELLO_ALLOWED_BOARD_IDS` env var — always wins
2. Saved config file (`~/.config/trello-mcp/config.json`)
3. Onboarding mode when neither is set

If the env var is set, the config file is ignored. Use the env var when you want explicit, portable MCP config. Use onboarding or the config file when you prefer zero board-id setup.

### MCP flow (Cursor, Claude, etc.)

Typical first-run conversation:

```
You:  Set up Trello for me
Agent: calls get_setup_status        → onboarding_required: true
Agent: calls list_available_boards   → [{ id, name, url }, ...]
Agent: asks which board(s) to use
You:  AS DEV
Agent: calls select_allowed_boards     → saves config, tools unlocked
You:  List cards in QA
Agent: calls list_cards                → works
```

Steps:

1. Connect with only `TRELLO_API_KEY` and `TRELLO_TOKEN`
2. `get_setup_status` — check if onboarding is needed
3. `list_available_boards` — list boards from your Trello account
4. User picks one or more boards by **name**
5. `select_allowed_boards` — pass the chosen **board ids**
6. Done — selection persisted, other tools enabled

### CLI flow (terminal)

If credentials are in `~/.cursor/mcp.json`, just run:

```bash
trello-mcp onboard
```

Credential sources (in order): shell env → `.env` → `~/.config/trello-mcp/.env` → `~/.cursor/mcp.json` → interactive prompt.

Or export manually:

```bash
export TRELLO_API_KEY=your_key
export TRELLO_TOKEN=your_token
trello-mcp onboard
```

For `~/.config/trello-mcp/.env`, use `KEY=value` or `export KEY=value` (both work).

Interactive prompt lists boards by number, id, or name. Re-run with `--force` to change the saved boards.

### Board id vs URL short code

Trello URLs look like `https://trello.com/b/Nh2gYTTn/as-dev`.

| Value | Example | Works in config? |
|-------|---------|------------------|
| Board id | `56c215abc7af5016b9bceadb` | yes |
| URL short code | `Nh2gYTTn` | no |

Onboarding returns the correct **board id** for each board. You do not need to copy it manually unless setting `TRELLO_ALLOWED_BOARD_IDS` by hand.

## MCP client configuration

All clients use the same pattern: spawn `trello-mcp` (or `npx`) over **stdio** and inject env vars.

### Minimal config (onboarding)

```json
{
  "mcpServers": {
    "trello": {
      "command": "npx",
      "args": ["-y", "@thadeu/trello-mcp"],
      "env": {
        "TRELLO_API_KEY": "your_key",
        "TRELLO_TOKEN": "your_token"
      }
    }
  }
}
```

Use `"command": "trello-mcp"` if installed globally.

### Full config (skip onboarding)

```json
{
  "mcpServers": {
    "trello": {
      "command": "npx",
      "args": ["-y", "@thadeu/trello-mcp"],
      "env": {
        "TRELLO_API_KEY": "your_key",
        "TRELLO_TOKEN": "your_token",
        "TRELLO_ALLOWED_BOARD_IDS": "56c215abc7af5016b9bceadb"
      }
    }
  }
}
```

Multiple boards: `"board_id_1,board_id_2"`.

### Cursor

`~/.cursor/mcp.json` or project `.cursor/mcp.json` — use either config above.

### Claude Desktop

`~/Library/Application Support/Claude/claude_desktop_config.json` (macOS):

```json
{
  "mcpServers": {
    "trello": {
      "command": "npx",
      "args": ["-y", "@thadeu/trello-mcp"],
      "env": {
        "TRELLO_API_KEY": "your_key",
        "TRELLO_TOKEN": "your_token"
      }
    }
  }
}
```

### Gemini CLI

```json
{
  "mcpServers": {
    "trello": {
      "command": "trello-mcp",
      "env": {
        "TRELLO_API_KEY": "your_key",
        "TRELLO_TOKEN": "your_token"
      }
    }
  }
}
```

Consult [Gemini CLI MCP docs](https://google-gemini.github.io/gemini-cli/docs/tools/mcp-server.html) for the exact config file path on your platform.

### OpenCode / Codex

```json
{
  "mcp": {
    "trello": {
      "type": "stdio",
      "command": ["npx", "-y", "@thadeu/trello-mcp"],
      "environment": {
        "TRELLO_API_KEY": "your_key",
        "TRELLO_TOKEN": "your_token"
      }
    }
  }
}
```

Adjust keys to match your client schema; the server binary and env vars stay the same.

## Tools

### Setup (always available)

| Tool | Description |
|------|-------------|
| `get_setup_status` | Check whether board onboarding is required |
| `list_available_boards` | All accessible boards (used during onboarding) |
| `select_allowed_boards` | Save user-selected boards and unlock other tools |

### Kanban (requires completed setup)

| Tool | Description |
|------|-------------|
| `list_boards` | Boards filtered by allowlist |
| `list_lists` | Lists on a board |
| `list_cards` | Cards on a board or list |
| `get_card` | Single card details **with attachments** (PR links, files) |
| `list_attachments` | Attachments on a card (GitHub PR links, URLs, uploads) |
| `list_card_members` | Members assigned to a card (id, username, full name) |
| `create_card` | New card in a list |
| `update_card` | Update name, description, due date, labels, archive |
| `move_card` | Move card to another list |
| `add_comment` | Add comment to a card |
| `archive_card` | Archive (close) a card |
| `add_attachment` | Attach a URL or local file to a card |

## Security

- Treat `TRELLO_TOKEN` like a password; never commit it.
- Restrict boards with onboarding or `TRELLO_ALLOWED_BOARD_IDS`.
- Use a service account token with access only to work boards.
- No delete-card tool in v1 to reduce accidental data loss.
- Server logs errors to **stderr** only; stdout is reserved for MCP protocol.

## Troubleshooting

### `no prebuilt binary for <platform>-<arch>`

npm did not install the platform package. Most common cause: installing with
`--no-optional` / `--omit=optional`. Reinstall without it:

```bash
npm install -g @thadeu/trello-mcp        # not --no-optional
```

Supported platforms are macOS and Linux on x64/arm64. The binary is native Go and
does **not** depend on your Node version — old-Node errors like
`SyntaxError: Unexpected token '??='` (from the previous JS build) no longer apply.

## Development

The server is written in **Go** (`cli/`). The root npm package is a thin launcher
plus build scripts (run with **Bun**).

```bash
cd cli && go run .          # run the server over stdio
cd cli && go test ./...     # tests
cd cli && go vet ./...      # vet
bun run build               # cross-compile all platforms → npm/<platform>/
bun run build:local         # only the current platform
bun run inspector           # build + MCP Inspector
```

Layout:

```
cli/                    Go MCP server (main, client, config, tools, onboard)
bin/trello-mcp.js       launcher: resolves the platform binary and execs it
scripts/build-binaries.ts  cross-compile + emit per-platform npm packages
npm/<os>-<arch>/        build output (gitignored): binary + package.json
```

## Release

Tag a version to cross-compile, publish, and create a GitHub release:

```bash
git tag v0.2.0
git push origin v0.2.0
```

Workflow `.github/workflows/release.yml`:

1. Cross-compiles the Go binary for every supported platform (`bun run build`)
2. Publishes each `@thadeu/trello-mcp-<os>-<arch>` package to **npm**
3. Publishes the root `@thadeu/trello-mcp` (with matching `optionalDependencies`)
4. Creates a GitHub release with generated notes

Bump `version` in `package.json` **and** the `optionalDependencies` versions together
(the build script fails if they drift).

### npm publish setup (one-time)

1. Create an npm account and ensure you own the `@thadeu` scope
2. Create an npm **Automation** token
3. Add repository secret `NPM_TOKEN` in GitHub → Settings → Secrets → Actions

## License

MIT — Copyright (c) 2026 Thadeu Esteves. See [LICENSE](LICENSE).
