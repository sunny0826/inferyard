# Core services (T4)

`Service` owns settings, device snapshots, run queue, download tasks and report
creation. `Options.Execute` defaults to the T2 CLI client; tests substitute a
fixture executor, never a real model server. HTTP cancellation is deliberately
not propagated to report generation. The application context owns background
work; cancellation uses T2 SIGINT followed by the two-second kill fallback.

## Files and ownership

- `types.go`: frozen v0 JSON field mirror; missing measurements use pointers/null.
- `service.go`: startup, fixed error messages/exit mapping, atomic settings file,
  executable probing, device refresh and symlink-aware path containment.
- `runs.go`: bound-config admission, atomic multi-config enqueue, serial worker,
  cancellation, resume validation, evidence detection and tail cursors.
- `aggregate.go`: event/case/resource projection and atomic history replacement.
- `reports.go`: report input validation, naming, generation, index and static path
  resolution. Report IDs use UTC `report-YYYYMMDDTHHMMSSZ[-N]`.
- `downloads.go`: independent concurrent acquisition, five-reading speed window,
  measured completion bytes/hash, retained failed outputs.
- `events.go`: 2048-event replay ring, startup-seeded IDs and coalesced wakeups.
  Slow clients rebuild from REST instead of silently dropping individual events.
- `core_test.go`: queue/exit mapping, device refresh, settings, downloads, reports,
  containment, resume parent chains, CPU deltas and replay load checks.

No additional dependency. Config admission reads the frozen scalar fields from
`config bind`'s generated TOML (JSON-encoded scalars, one assignment per line).
The CLI performs final config and execution validation. Results/models settings
cannot be cleared; the binary override can be null. Persisted settings live at
`os.UserConfigDir()/InferYard Studio/settings.json`; effective values are computed
per request. Model-root changes invalidate the cached snapshot and discard a
stale in-flight result. Results-root changes rebuild outside the service mutex
and publish the completed index atomically. Canonical paths account for macOS
`/var` versus `/private/var` and existing symlink ancestors.

Run progress consumes `inputs.documents.selection.payload.case_ids`, including
resume subsets, and joins terminal/score events by request ID. Resource history
retains 600 points. CPU deltas require matching process/clock identity, positive
clock frequency and elapsed time, nondecreasing counters and no sampling gap
above two seconds; otherwise the value is null. Progress publishes at most 2 Hz;
resource publication batches approximately once per second, with a final flush.

Resume follows plan trial order and each trial's parent/resume chain, not directory
mtime or lexical run ID. The CLI remains the final authority. History never
reads summary.json. Missing or malformed evidence cannot establish completion;
malformed run/batch entries are skipped individually and counted in the index.
Filesystem traversal failures and unreadable or malformed host state return a
fixed internal error.

## Q4: implemented dirty criterion and proposed contract patch

Read-only source references in the inferyard repository:

- `src/inferyard/runtime/lock.py`: `STATE_PATH`, `HostLock.dirty`, `clean`.
- `src/inferyard/platforms/windows_host_paths.py`: system common-appdata lookup.
- `src/inferyard/runtime/batch_state.py`: plan order and resume ancestry.
- `src/inferyard/runtime/batch_runner.py`: `_next` and resume final validation.
- `src/inferyard/evidence/trial_summary.py`: completion conditions.

`internal/evidence/dirty.go` reads the crash-persistent host marker and requires
schema version 1, `dirty=true`, matching `run_id`, and `kind="run"`. Unix uses
`/var/tmp/inferyard-host.state.json`; Windows uses SHGetFolderPathW's system common
appdata directory, independently of environment overrides. No lock is acquired
and neither marker nor evidence is changed. `RunStatus` applies the frozen
precedence: cancelled > dirty > partial > complete.

**Contract patch proposal, §4.4 / Q4 (not applied):**

> dirty 表示读取时主机持久状态 `inferyard-host.state.json` 的
> `schema_version=1`、`dirty=true`、`kind="run"` 且 `run_id` 与该 run 匹配。
> Unix 状态目录为 `/var/tmp`，Windows 为系统 Common AppData 已知目录。
> 状态缺失表示当前无可匹配标记；状态不可读或格式非法返回 `internal_error`，
> 不推断为 clean。标记经 clean 或后续任务替换后，历史 run 不再因该标记呈现 dirty；
> 这不证明其历史 drain 已核验，也不代表证据完整。取消状态仍优先于 dirty。

## Verification

2026-10-12, `GOTOOLCHAIN=go1.27.2`,
`GOPROXY=https://goproxy.cn,direct`, command-scoped `CGO_ENABLED=0`.

Tests exercise all exit codes 0/2/3/4/130, queued cancellation with null exit,
serial execution, refresh conflict and exit-2 snapshot passthrough, settings
persistence, simultaneous downloads, occupied output rejection, static path
containment, duplicate report rejection, report collision naming and missing
source links, plan-order resume ancestry, host marker matching and CPU reset
nulls. Server tests additionally prove report completion after client disconnect
and fixture-run SSE delivery. See `../server/README.md` for commands and Q5 data.

No real benchmark, model service or network model download was started. Native
Windows/Linux runtime behavior is not established by cross-compilation. This
projection is a UI index, not inferyard evidence/hash verification; the CLI owns
final resume and report validation.
