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

The suite resolves the CLI in order: `INFERYARD_E2E_CLI`, `PATH`, the synced
in-repo environment (`cli/.venv`), then a `uv run` wrapper (POSIX only). It
skips when none works. Set `INFERYARD_E2E_ROOT=<dir>` to preserve the
workspace and blame transcript for post-mortem inspection instead of
cleaning them up.

## Layout

| File | Responsibility |
| --- | --- |
| `main_test.go` | Test-binary re-exec: argv with `--params-file` becomes the fake engine |
| `fakeengine.go` / `fakeengine_server.go` | Fake llama-server: the seven endpoints the prism adapter verifies, byte-length token accounting |
| `gguf.go` | Minimal valid GGUF so the real device-check discovers one local model |
| `candidate.go` | Fabricates the `config_candidate.v1` pair (the deliberate fixture boundary) |
| `env.go` | Isolated settings/host state, CLI resolution, `INFERYARD_E2E_ROOT` |
| `recorder.go` | Executor middleware: `transcript.jsonl` plus per-call stdout/stderr with sha256 |
| `schemagate.go` | Validates every schema-covered evidence document against `cli/schemas/` |
| `blame.go` | Failure attribution from the transcript (see below) |
| `chain_test.go` | Golden chain: init → device-check → bind → run both bundles → verified report |
| `scenarios_test.go` | Command guards, queued-run cancellation, engine crash recovery |

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
- Window-level assembly (`ui.NewTester`) and real-window smoke are out of
  scope: page logic is covered by `internal/pages` unit tests, and native
  acceptance stays manual per `studio/README.md`.

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
the project's frozen uv environment. With the retained result, page projection can be checked:

```sh
STUDIO_SPEED_RESULT=/absolute/new-run/output/result.json mise exec -- go test ./internal/pages -run TestContextSpeedRealCLIProjection -count=1
```
