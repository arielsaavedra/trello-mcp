# SABIZ fork: installation and maintenance

This repository is a fork of [thadeu/trello-mcp](https://github.com/thadeu/trello-mcp), retaining the MIT license and attribution. Fork releases build a local Go executable; they do not publish packages under the upstream author's npm namespace.

## Install on a Mac

```sh
brew install go python
gh repo clone arielsaavedra/trello-mcp
cd trello-mcp
sh scripts/build-local.sh
```

Go 1.25+ builds the server. Python 3.11+ is needed only for setup/verification helpers. Node and the upstream npm package are not needed to run the built fork.

If `mcp-trello` is already registered in Codex:

```sh
python3 scripts/configure_codex.py
```

This switches only the command/arguments, preserves existing environment and other settings, and saves a protected rollback copy outside Git. It does not expand board access.

For a fresh installation, use Codex Settings → MCP servers → Add server, choose STDIO, and set:

- Name: `mcp-trello`
- Command: the absolute path to `dist/trello-mcp` in this clone.
- Environment: `TRELLO_API_KEY`, `TRELLO_TOKEN`, and `TRELLO_ALLOWED_BOARD_IDS` with the exact full board IDs authorized by the user.
- `TRELLO_API_SCOPE=boards` (default).

Obtain the API key and user token through [Trello's authorization flow](https://developer.atlassian.com/cloud/trello/guides/rest-api/authorization/). Enter credentials locally, never into a committed file, chat, terminal command history, or screenshots. Tokens remain subject to their permissions, expiry, and Trello role restrictions. Board IDs are configuration, not credentials.

Restart the MCP server or Codex after changing its command. An already-open conversation may need a new task to discover newly registered tools. A successful standalone protocol test does not prove that the current conversation has refreshed its tool catalog.

After configuration, run `python3 scripts/smoke_test.py`. This launches the configured server, checks the allowlist and reads cards/comments/history without modifying Trello. It prints only counts and capabilities. `--binary dist/trello-mcp` tests a build before switching Codex. Do not interpret zero returned comments/history as an error; inspect coverage and pagination for the chosen card.

## API coverage and scope

The bundled catalog contains all **261 operations / 191 paths** in the official Trello OpenAPI snapshot fetched on 2026-09-14. Use `search_api_endpoints` and `get_api_endpoint`, then `trello_api_read` for GET or `trello_api_write` for POST/PUT/PATCH/DELETE. Supported request encodings: query, JSON, form, and multipart uploads. Responses preserve JSON, text, or base64 for other content; responses over 8 MiB fail explicitly. Pagination is controlled by each endpoint's documented parameters, never silently assumed complete.

Coverage means the connector can address every cataloged operation. It does **not** establish that every operation is available to an API-key token, that your account has the required role/plan, or that every operation has been live-tested. Private/undocumented Trello APIs are outside this catalog.

`boards` scope is the safe default for an existing board workflow. Named tools preserve the allowlist. Generic operations validate board ownership for board/card/list/checklist/label/custom-field/action resources and referenced source/destination IDs. Account-wide resources, collection creation, search, generic field writes and ambiguous cross-resource operations fail closed. Use dedicated tools for card creation/copying/movement. GET `/batch` is supported from `0.3.0-sabiz.4` after validating every subrequest; see [BATCH.md](BATCH.md). The catalog remains searchable regardless of scope.

For a future task explicitly authorized to manage the full account, configure a **separate MCP entry** with `TRELLO_API_SCOPE=account`. Its generic API tools can address all cataloged routes, including workspaces, members, enterprises, notifications, plugins, tokens, webhooks and batch/search. This explicitly bypasses the generic tools' board restriction; do not change a board-scoped LoanFlow entry to account scope merely to make a failed call work. Trello's own authorization still applies. Posting messages and destructive operations require the user's authorization for the actual action.

Full-API support is exposed through a small catalog-driven tool set instead of hundreds of tools loaded into every conversation. Common board operations keep their concise named tools.

## LoanFlow additions

- `list_card_comments`: original text, action ID, author username/ID and timestamp.
- `list_card_history`: create/copy/list-change/board-transfer evidence. Follow `next_before` while `has_more`; a full page conservatively indicates another page may exist. Omit `since` when establishing list age. Creation/last activity timestamps are not substitutes for uninterrupted time in a list.
- `list_board_members`: mentions can be resolved even when members are not assigned to the card.
- `list_cards(include_closed=true)`: archived duplicate checks, template flag, position and last activity.
- `copy_template_card`: copies a verified template on the same board, including hidden/archived templates, preserving its description/checklists without copying comments, attachments, members or due dates.
- `move_card(position="top")`: first position in the destination list.
- An explicit environment allowlist cannot be widened through onboarding tools.

## Update and test

```sh
git fetch upstream
git log --oneline HEAD..upstream/main
# Review upstream changes, then merge when appropriate:
git merge upstream/main
cd cli
go test -race ./...
go vet ./...
cd ..
sh scripts/build-local.sh
git diff --check
git add <reviewed-files>
git commit -m "Describe the change"
git push origin main
```

Do not auto-merge incompatible upstream changes or discard local edits. Refresh the catalog explicitly with `python3 scripts/update_api_catalog.py`, review the diff, run tests, and commit the resulting snapshot. The source URL/hash are embedded in `cli/trello-openapi.json`; example values are omitted. Official API definitions retain their source provenance and are not authored by this fork.

## Cleanup and rollback

Before deleting files or uninstalling software, list the exact targets, explain why they are obsolete, and obtain user confirmation. Keep this source clone, Git history, `dist/trello-mcp`, credentials, and Go (needed for rebuilds). An npm-installed upstream connector becomes optional once the fork is verified. Uninstalling that package does not remove the fork's source or binary.

Do not automatically delete the protected Codex rollback copy. For rollback, restore only the previous `mcp-trello` command/args after inspecting newer configuration; blindly restoring the entire file could discard unrelated changes. Reinstall the old npm package if it has already been removed. Document every approved cleanup and verify its result.
