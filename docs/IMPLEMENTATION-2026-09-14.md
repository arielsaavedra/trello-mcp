# Implementation record — 2026-09-14

## Outcome

Created `arielsaavedra/trello-mcp` as a fork of `thadeu/trello-mcp`, starting at upstream commit `501d92c` (v0.2.10). Preserved upstream MIT attribution. Added version `0.3.0-sabiz.1`, a native local build, and a complete catalog of the 261 operations in the public Trello OpenAPI snapshot. No npm package was published.

Implemented paginated comment and movement-history reads, member resolution, template copying, first-position moves, archive-aware inventories, full REST discovery/read/write tools, JSON/form/multipart support, fail-closed board scope, explicit full-account opt-in, and transport error handling that excludes credential-bearing URLs. Locked allowlists cannot be changed through onboarding tools. Corrected list-card reads to validate that a supplied list belongs to the selected board.

Generic REST parameters are described by the bundled official schema; they are not a promise that every account or token can use every endpoint. The server validates endpoint/method, path construction and configured scope. Trello performs endpoint-specific field/role/plan validation. No undocumented API is advertised.

## Steps and terminal commands

Commands were executed in the indicated repositories; credentials were never embedded in command text. The user-facing session report contains the chronological terminal command ledger, including inspection and failed diagnostic commands. This public record excludes account/board/client details and local credential paths beyond standard setup locations.

```sh
gh auth status
gh repo view thadeu/trello-mcp --json nameWithOwner,licenseInfo,defaultBranchRef,url
gh repo fork thadeu/trello-mcp --clone=false
gh repo clone arielsaavedra/trello-mcp <local-project-directory>
brew install go
```

Read the existing client, tools, configuration, license and CI files before modifying them. Source/docs were edited using the file patch tool, not secret-bearing shell substitutions. Homebrew automatically refreshed itself during Go installation; its warnings about unrelated legacy taps were not acted on and no unrelated package was explicitly upgraded/uninstalled.

```sh
python3 scripts/update_api_catalog.py
cd cli
gofmt -w actions.go actions_test.go templates.go client.go tools.go config.go api.go api_test.go
go test -race ./...
go vet ./...
cd ..
sh scripts/build-local.sh
python3 scripts/test_setup.py
python3 scripts/smoke_test.py --binary dist/trello-mcp
python3 scripts/configure_codex.py
python3 scripts/smoke_test.py
git diff --check
```

On the installation host, Python helpers used an existing Python 3.11+ runtime because the default `python3` was 3.9. The setup helper switched only the existing MCP command/arguments, preserved credentials and other settings, and created a protected rollback copy next to Codex's config. The source and executable remain separate from the dependent LoanFlow skill.

## Validation

- Go tests cover pagination/evidence preservation, scope enforcement, invalid cursors, upstream errors, foreign-list rejection, template copying and positioned moves against a simulated API, URL error redaction, official catalog coverage, JSON/form/multipart encodings, and actual MCP tool dispatch.
- Python setup tests verify credential/setting preservation, rollback permissions, and idempotent reconfiguration.
- Initial tests found an inefficient repeated catalog parse and an incorrect `/boards` test assumption: the official create-board path is `/boards/`. The parser now loads the catalog once and the test uses the exact published path. Subsequent tests passed.
- Live read-only testing verified the configured board, lists, members, card inventory, card/checklist details, paginated comments/history and a generic REST read. No live write, delete, external message or workflow sync was executed. Write behavior was tested with synthetic data through a simulated HTTP transport.
- Source review excludes real credentials and borrower records. CI runs Go race tests/vet/build and Python setup tests; the upstream npm publishing workflow is disabled for this fork by repository condition.

## Maintenance and cleanup

Use [SETUP.md](SETUP.md) for installation, updating from upstream, scope choices, rebuilding, testing and rollback. The user's standing instruction for this project is to document changes and terminal commands, propose cleanup at the end, and obtain confirmation before deleting files or uninstalling packages. The old npm connector is a cleanup candidate only after this build is verified; it was not removed during implementation. Keep Go, the active binary/source, credentials and the protected rollback copy until their removal is explicitly approved.
