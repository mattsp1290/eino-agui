#!/bin/sh
set -eu

: "${AG_UI_REPO_DIR:?set AG_UI_REPO_DIR to an AG-UI git checkout containing aaa75b54d572}"
target=aaa75b54d572be8cd1d51c72e951273c5b893ed0
fixture_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
work_dir=$(mktemp -d)
trap 'rm -rf "$work_dir"' EXIT HUP INT TERM

git -C "$AG_UI_REPO_DIR" cat-file -e "$target^{commit}"
git -C "$AG_UI_REPO_DIR" archive "$target" | tar -x -C "$work_dir"
test_path="$work_dir/sdks/typescript/packages/client/src/apply/__tests__/eino-agentic-conformance.test.ts"
cp -f "$fixture_dir/eino-agentic-conformance.test.ts" "$test_path"

cd "$work_dir"
if command -v corepack >/dev/null 2>&1; then
  set -- corepack pnpm
elif command -v pnpm >/dev/null 2>&1; then
  set -- pnpm
else
  set -- npx --yes pnpm@10.33.4
fi
COREPACK_ENABLE_DOWNLOAD_PROMPT=0 "$@" install --frozen-lockfile
COREPACK_ENABLE_DOWNLOAD_PROMPT=0 "$@" --filter @ag-ui/client exec vitest run src/apply/__tests__/eino-agentic-conformance.test.ts
echo "AG-UI client conformance passed at $target"
