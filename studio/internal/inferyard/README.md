# T2 integration handoff

`Locate(ctx, override)` resolves an executable and parses `inferyard VERSION`.
`Client.Exec(ctx, args...)` passes argv directly, captures both streams and returns
CLI exit status separately from launch/I/O/context errors. Nonzero CLI exits do
not discard stdout. Cancellation returns the context error and `Cancelled`, while
`ExitCode` retains the actual process exit (including -1 for a signal). Unix
children use a separate process group: SIGINT first, SIGKILL after the grace
period (default two seconds). Windows uses Kill.

`Parse` accepts a single UTF-8 JSON command object. `DeviceCheck` invokes only
`device-check --json`; `ParseDeviceCheck` exposes the four preflight sections as
raw JSON, retaining nested fields without reimplementing the upstream schema.

The sibling evidence package provides:

- `Index(root)`: layout2 runs, direct run roots and batch.json/runs containers.
  Malformed/unreadable entries are skipped and counted. Sealed is the manifest
  declaration; this index does not perform inferyard's evidence verification.
- `Tail(path, cursor)`: complete JSONL records with increasing seq. Persist both
  offset and seq per file. Incomplete final lines remain unread. Corruption
  returns the valid prefix and its cursor. A shortened file returns ErrTruncated;
  the caller can explicitly restart with an empty cursor. Streams must retain
  their append-only identity; replacing a file in place is outside this contract.
- `PollDownload(ctx, dir, interval, notify, targets...)`: reports observed part
  sizes and corresponding regular-file publication. Pass expected target
  basenames to cover downloads completed before the first poll. Without targets,
  only observed parts are tracked. Empty directories and disappearing parts do
  not mean success. Cancel polling when the CLI exits unsuccessfully. File
  publication does not certify successful completion of the overall CLI command.

## Validation (2026-10-12)

All Go checks used `GOTOOLCHAIN=go1.27.2`,
`GOPROXY=https://goproxy.cn,direct`, `CGO_ENABLED=0`.

| Command | Exit |
| --- | --- |
| `go test ./...` | 0 |
| `go vet ./...` | 0 |
| `go test -race ./...` | 0 |
| `GOOS=windows GOARCH=amd64 go build ./internal/inferyard ./internal/evidence` | 0 |
| `GOOS=linux GOARCH=amd64 go build ./internal/inferyard ./internal/evidence` | 0 |
| `INFERYARD_SMOKE_ROOT=/Users/guoxudong/codes/local-ai/inferyard go test ./internal/inferyard -run '^TestRealCLI$' -v -count=1` | 0 |

The opt-in smoke test executed exactly these command forms from the inferyard
repository and parsed their stdout through the Go parsers:

- `mise exec -- uv run --frozen inferyard --version`: exit 0, version parsed.
- `mise exec -- uv run --frozen inferyard device-check --json`: exit 0, command
  envelope and device_preflight.v1 parsed.

Unit tests cover 0/2/3/4/130/unknown exits, SIGINT handler output and exit 130,
forced termination when SIGINT is ignored, literal argv, PATH/override lookup,
invalid JSON, device sections, index corruption/seals, both JSONL streams,
append/partial lines/resume/truncation, download growth/rename/missing directories,
removed parts, cancellation and already published targets.

The four-file testdata run is a hand-built indexing/tailing fixture, not a fully
verified eight-file benchmark evidence bundle. Windows/Linux runtime cancellation,
real benchmark runs and real downloads were not exercised. No blocking items.

`progress.go` 解析独立的 `progress.v1` 对象，不使用通用 CommandResult 外壳，也不推导分数或
证据完成状态。`Client.Progress` 是可单测的 CLI 边界；Studio 服务使用相同解析器和 core
注入式 executor。`testdata/progress.v1.json` 与测试中的各阶段变体提供模拟契约证据。
`command.go` 只切分向导命令的引号与参数，不调用 shell、不执行变量替换。
