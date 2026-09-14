#!/bin/sh
set -eu
project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$project_dir/cli"
mkdir -p "$project_dir/dist"
fork_version=$(cat "$project_dir/VERSION")
go build -trimpath -ldflags "-X main.version=$fork_version" -o "$project_dir/dist/trello-mcp" .
printf 'Built %s\n' "$project_dir/dist/trello-mcp"
