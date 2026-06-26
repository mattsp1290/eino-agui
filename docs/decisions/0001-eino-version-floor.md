# Decision 0001: eino Version Floor

Date: 2026-06-26

## Decision

Use `github.com/cloudwego/eino v0.8.13` as the minimum supported version for the first
`eino-agui` library extraction, and test the library against both:

```text
github.com/cloudwego/eino v0.8.13
github.com/cloudwego/eino v0.9.2
```

The reference app already uses v0.9.2, so v0.9.2 remains a required compatibility target. It does
not need to be the module floor for the core seam identified so far.

This decision should be revisited only if the pending ensemble backend diff in Decision 0002 proves
that the shared surface needs an API absent from v0.8.13.

## Symbol Availability

The task graph called out these possible v0.9.x-only dependencies. All of them are present in
v0.8.13 and v0.9.2:

| Symbol or behavior | v0.8.13 | v0.9.2 | Evidence |
| --- | --- | --- | --- |
| `schema.MessageInputPart` | Present | Present | `schema/message.go` defines `type MessageInputPart` in both versions. |
| `schema.MessageInputImage` | Present | Present | `schema/message.go` defines `type MessageInputImage` in both versions. |
| `schema.Message.UserInputMultiContent` | Present | Present | `schema.Message` includes `UserInputMultiContent []MessageInputPart` in both versions. |
| `schema.ChatMessagePartTypeImageURL` | Present | Present | `schema/message.go` defines `ChatMessagePartTypeImageURL = "image_url"` in both versions. |
| `schema.Message.ReasoningContent` | Present | Present | `schema.Message` includes `ReasoningContent string` in both versions; streamed chunks are `*schema.Message` values, so this covers the `chunk.ReasoningContent` concern from the planning prompt. |
| `schema.ConcatMessages` | Present | Present | `schema/message.go` defines `func ConcatMessages(msgs []*Message) (*Message, error)` in both versions. |
| Extra-merging `schema.ConcatMessages` | Present | Present | Both versions collect non-empty `msg.Extra` maps, merge them through `concatExtra`, and assign the merged map to `ret.Extra`. |

Commands used:

```bash
go get github.com/cloudwego/eino@v0.8.13
go list -m -json github.com/cloudwego/eino

go get github.com/cloudwego/eino@v0.9.2
go list -m -json github.com/cloudwego/eino

rg -n 'type MessageInputPart|type MessageInputImage|UserInputMultiContent|ChatMessagePartTypeImageURL|ReasoningContent|func ConcatMessages' \
  /Users/punk1290/git/pkg/mod/github.com/cloudwego/eino@v0.8.13/schema \
  /Users/punk1290/git/pkg/mod/github.com/cloudwego/eino@v0.9.2/schema
```

Resolved module metadata:

```text
v0.8.13: Time 2026-04-29T06:14:58Z, Sum h1:z5dhaZNN8TWZbP/lgKxGmF26Ii8fPeUlQCGV/NTtms0=
v0.9.2:  Time 2026-05-28T09:40:30Z, Sum h1:q9nsOy79UAs2yiCpVLzEzIOyv1BWbiP1rrdmNcv1wf0=
```

## Consumer Impact

Known local consumers:

| Repository | Current eino pin | Current tests | Forced v0.9.2 temporary-copy tests |
| --- | --- | --- | --- |
| `/Users/punk1290/git/ag-ui-go-server-example` | v0.9.2 | `go test ./...`: pass | Already on v0.9.2. |
| `/Users/punk1290/git/eino-providers` | v0.8.13 | `go test ./...`: pass | `go get github.com/cloudwego/eino@v0.9.2 && go test ./...`: pass. |
| `/Users/punk1290/git/eino-tools` | v0.8.13 | `go test ./...`: pass | `go get github.com/cloudwego/eino@v0.9.2 && go test ./...`: pass. |

These results show that the known local v0.8.13 modules can absorb a v0.9.2 MVS bump in their
current state. They do not prove the ensemble backend can absorb that bump, because its repository
has not yet been located and audited.

## Rationale

Choosing v0.8.13 as the library floor avoids forcing a v0.9.2 upgrade on the not-yet-located
ensemble backend before its build and duplicated seam are inspected. The currently planned shared
surface can still use multimodal input parts, image URL parts, reasoning content, tool calls, and
`ConcatMessages` without requiring v0.9.2.

The extraction tasks should therefore:

- Set the initial `go.mod` floor to `github.com/cloudwego/eino v0.8.13`.
- Include compatibility verification against v0.9.2, because the reference app already uses it.
- Once this repo has a Go module, run the compatibility matrix in temporary worktrees or module
  copies:

  ```bash
  go get github.com/cloudwego/eino@v0.8.13 && go test ./...
  go get github.com/cloudwego/eino@v0.9.2 && go test ./...
  ```

- Avoid adding v0.9.x-only APIs unless a later task records a new decision with evidence.
- Treat adjacent APIs introduced after v0.8.13, such as newer input-part variants not listed in the
  symbol table above, as outside this approved floor until separately audited.
- Treat the ensemble backend audit as the final adoption proof, not as already satisfied by local
  provider/tool tests.

If the ensemble backend cannot compile against v0.9.2, this floor prevents `eino-agui` from being
the component that forces that upgrade. If the ensemble backend needs v0.9.2-only APIs after its
diff is complete, update this decision before extraction work depends on the higher floor.
