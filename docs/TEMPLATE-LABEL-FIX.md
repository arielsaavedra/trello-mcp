# Template copy labels — 0.3.0-sabiz.3

The dedicated copy tool now sends all source label IDs during creation and explicitly places the copy at the bottom. It retains the same allowed-board, template and destination-list validation. The generic collection API scope rules are unchanged.

Validation: `gofmt -w templates.go actions_test.go` and `go test ./...` from cli; `sh scripts/build-local.sh` from the repository root. The existing mock test checks both labels and bottom position. A fresh STDIO MCP instance verified board scope and successfully copied an authorized template with labels and checklists. Restart an already-running MCP process to load the updated executable.
