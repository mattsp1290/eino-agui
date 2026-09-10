#!/bin/sh
set -eu

if [ "$#" -ne 1 ]; then
  echo "usage: $0 <immutable-eino-agui-version-or-commit>" >&2
  exit 2
fi

pin=$1
case "$pin" in
  /*|.*|*..*|*\\*)
    echo "bridge pin must be an immutable remote version or commit, not a path" >&2
    exit 2
    ;;
esac

fixture_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
work_dir=$(mktemp -d)
trap 'rm -rf "$work_dir"' EXIT HUP INT TERM
cp -f "$fixture_dir/consumer_test.go" "$work_dir/consumer_test.go"
cd "$work_dir"

GOWORK=off go mod init example.com/eino-agui-clean-consumer
GOWORK=off go mod edit -require="github.com/mattsp1290/eino-agui@$pin"
GOWORK=off go mod edit -replace="github.com/ag-ui-protocol/ag-ui/sdks/community/go=github.com/mattsp1290/ag-ui/sdks/community/go@v0.0.0-20260909025854-aaa75b54d572"
GOWORK=off go mod tidy
GOWORK=off go test ./...
GOWORK=off go list -m -json all > modules.json
GOWORK=off go list -m -f '{{if .Replace}}{{if not .Replace.Version}}LOCAL {{.Path}} => {{.Replace.Path}}{{end}}{{end}}' all | sed '/^$/d' > local-replacements.txt

if test -s local-replacements.txt; then
  echo "clean consumer module graph contains a filesystem replacement:" >&2
  sed -n '1,20p' local-replacements.txt >&2
  exit 1
fi

echo "clean consumer passed for github.com/mattsp1290/eino-agui@$pin"
echo "required root replacement: github.com/ag-ui-protocol/ag-ui/sdks/community/go => github.com/mattsp1290/ag-ui/sdks/community/go@v0.0.0-20260909025854-aaa75b54d572"
