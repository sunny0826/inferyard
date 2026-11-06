# Studio e2e suite

Boundary-level end-to-end tests for Studio: the real CLI executes every
operation, the engine is a fake (the test binary re-executing itself), and
every CLI call is recorded so a failing step states which side owns the
fault.

## Running

From `studio/`:

```sh
mise exec -- go test ./tests/e2e/ -count=1 -timeout 900s -v
```

The suite resolves the CLI in order: `INFERYARD_E2E_CLI`, a `PATH` entry whose
`config describe --help` succeeds, the synced in-repo environment (`cli/.venv`),
then a `uv run` wrapper (POSIX only). It skips when none works. An older installed
CLI that does not accept `config describe` is skipped rather than used. Set
`INFERYARD_E2E_ROOT=<new-dir>` to preserve the workspace, host files and blame
transcript for post-mortem inspection instead of cleaning them up. Use a fresh
root for each test invocation so recorded evidence is not overwritten.

`NewEnv` creates a unique `host-*` directory per test. It copies the embedded
`cli_bootstrap.py` there as `sitecustomize.py` and prepends that directory to
`PYTHONPATH`, retaining the inherited value. Every real CLI subprocess in that
test uses the same temporary `LOCK_PATH` and `STATE_PATH`; Studio's
`Options.HostStatePath` reads that same state. HostLock, dirty-state handling and
identity checks still execute normally. Bootstrap failure stops Python instead
of continuing with the global host files.

The original Python CLI entry point is used, including `inferyard.exe` on
Windows; it must allow Python's normal `sitecustomize` startup. This injection
is test-only and does not change the production fixed-path contract. `t.Setenv`
restores the environment after each test and forbids parallel tests; callers of
`NewEnv` and their ancestors must not use `t.Parallel`.

## Layout

| File | Responsibility |
| --- | --- |
| `main_test.go` | Test-binary re-exec: argv with `--params-file` becomes the fake engine |
| `fakeengine.go` / `fakeengine_server.go` | Fake llama-server: the seven endpoints the prism adapter verifies, byte-length token accounting |
| `gguf.go` | Minimal valid GGUF so the real device-check discovers one local model |
| `candidate.go` | Fabricates the `config_candidate.v1` pair (the deliberate fixture boundary) |
| `env.go` | Isolated settings and shared per-test CLI host files, CLI resolution, `INFERYARD_E2E_ROOT` |
| `cli_bootstrap.py` | Test-only Python startup injection for the real CLI host paths |
| `recorder.go` | Executor middleware: `transcript.jsonl` plus per-call stdout/stderr with sha256 |
| `schemagate.go` | Validates every schema-covered evidence document against `cli/schemas/` |
| `blame.go` | Failure attribution from the transcript (see below) |
| `chain_test.go` | Golden chain: init → device-check → bind → run both bundles → verified report |
| `scenarios_test.go` | Command guards reject invalid requests before launching CLI work |

## Blame model

The CLI owns everything up to and including its stdout bytes; Studio owns
what it does with a well-formed answer. `analyzeFailure` classifies a failing
step from the last transcript record:

- exit ≠ 0, or exit 0 with stdout that is not valid JSON → **cli**;
- exit 0 with valid JSON but wrong downstream behavior → **studio**;
- no CLI call recorded → Studio failed before reaching the boundary.

`assertEvidenceContracts` adds the artifact half of the gate: a contract
violation in workspace evidence is CLI-side by construction, because Studio
never writes those files. Roughly 1.8k documents and journal lines are
checked in the golden chain.

## Fixture boundaries and non-goals

- Candidate production (`config create`) is fabricated by `WriteCandidate`;
  it needs real engine assets and is covered elsewhere.
- The candidate admits any environment (`environment-admission.v2` with empty
  `required_fields`): `ac_online`/`profile`/`governor`/`epp` observability is
  platform-dependent and a static fixture cannot match it portably. Observed
  differences are still recorded in evidence.
- Device check runs for real against the fake GGUF; only the model is a
  fixture, not the hardware probe.
- Queue cancellation and engine-process failure behavior use controlled core tests;
  this suite does not claim separate real-CLI cancellation or crash scenarios.
- Window-level assembly (`ui.NewTester`) and native windows are out of scope:
  page logic is covered by `internal/pages`, while [native UI e2e](../ui/README.md)
  drives the actual window with a simulated backend.

## Context-speed boundary

`TestContextSpeedPublicCLI` runs `context_speed_bridge.py` through mise's uv and the frozen
CLI environment. It reuses the CLI localhost Prism fixture and publicly dispatches
plan → five trials → speed-results. Human review and host observations are explicit
test fixtures; an unapproved generated corpus is supplied at the resource boundary to
check that init refuses with `human_corpus_review_required` / 4 and creates no output.
The shipped corpus has separate human approval. Original hashes, model-request
counts and absence of HTML are checked after offline reading. The production Go parser
consumes that JSON; this is not real model or context-capacity acceptance.

Set `STUDIO_SPEED_E2E_ROOT` to a new directory to retain the fixture and `output/result.json`.
Do not reuse an existing output directory. Set `UV_PYTHON` to `mise which python` and use
the project's frozen uv environment. Page tests verify their own result, missing-data
and retry expectations; the native UI suite verifies interaction with the simulated results.
