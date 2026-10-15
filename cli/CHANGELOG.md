# Changelog

## Unreleased

### Added

- A new read-only `service discover` command enumerates TCP loopback listeners
  and probes each endpoint with `GET /health` and `GET /v1/models` (2s timeout),
  reporting the owning process PID, executable name and start ticks per listener
  without filtering by process name. It never sends model requests, records only
  status-code classes and model id lists, marks ports owned by multiple
  processes as ambiguous instead of picking one, and exits 3 with limitations
  when enumeration is partial. Discovery does not establish bind eligibility:
  `config bind` still requires an explicit `--pid` and performs the full
  identity and argv checks. Output schema is registered as
  `--schema service_discovery`.
- A new read-only `engine check` command re-verifies a local engine with the
  same checks as `config create`: the bounded `--version` subprocess query
  against the frozen version pattern (30s timeout; macOS exec-time validation
  of freshly extracted ad-hoc-signed binaries can reach ~12s), the macOS dylib
  manifest rules and the Windows runtime receipt verification. It never
  creates directories, writes files, starts services or sends model requests.
  The input surface is platform-asymmetric: macOS requires `--engine FILE`
  and Windows requires `--runtime-receipt FILE`; mismatched or conflicting
  arguments exit 2. Eligible and ineligible outcomes both exit 0 — an explicit
  deviation from the device-check family's blocked→2 semantics, because this
  query blocks nothing and `config create` remains the final authority — with
  ineligibility reported as one fixed reason from the contract table; tool
  errors exit 4 with redacted diagnostics. On macOS, where no device preflight
  selects a mode, details report per-mode results `modes: {cpu, metal}` (metal
  requires the metal dylib in the manifest); Windows reports the verified
  receipt profile `mode` (`cpu`/`cuda`). `reported_version` carries the
  engine's own measured version line while `expected_release` is honestly
  labeled as the expected constant. A passing check never confers candidate
  eligibility. Output schema is registered as `--schema engine_check`.

## 0.0.3

### Added

- A new read-only `progress --workspace DIR` command emits a `progress.v1`
  onboarding snapshot (exit 0/2 only): which first-run steps are done for a
  workspace — configuration, bound engine and model, runs and reports — so a
  desktop wizard or operator script can resume setup without parsing
  preparation directories. It never starts services or sends model requests.
- `config create --workspace DIR` derives preflight, results and bundle paths
  from the workspace layout instead of requiring each path explicitly;
  explicit arguments keep precedence and legacy behavior is unchanged.
- `init` now extends the exported workspace layout with `preflight/`,
  `candidates/`, `bound/` and `reports/` directories plus a roadmap README
  that substitutes `{{WORKSPACE}}` at export time.
- The repository now ships InferYard Studio (`studio/`), a Go desktop panel
  with a native first-run wizard driving the CLI onboarding chain (workspace
  readiness and provisioning, one-tap CLI installation, built-in test model
  and engine assets, acquire, device-check, candidate, bind, run and
  report/verify steps). Studio is a separate module and is not part of the
  Python package.

### Changed

- The repository is now a monorepo: the Python CLI moved under `cli/`,
  alongside `studio/` and `observer/`; CI workflows and documentation paths
  follow the new layout. The distributed package name, entry points and the
  active data contract (`schema_version = 3`) are unchanged.
- `config create` now allows a bounded 30-second default for the engine
  version query: macOS exec-time validation of freshly extracted
  ad-hoc-signed binaries and their dylib chain can stall past the previous
  fixed 10-second budget (observed ~12s during onboarding acceptance,
  matching the known e2e first-exec behavior). The frozen runtime-prepare
  profile keeps its 10-second budget, and failures still map to
  `engine_version_query_failed`.

## 0.0.2

### Added

- A new `engine acquire` command downloads a single engine file from a
  caller-supplied HTTPS URL into a fresh output directory (`engine acquire
  --source URL --out NEW`), streaming bytes to a `.part` file and publishing
  the file plus an `engine-source.json` receipt (`remote_engine_source.v1`)
  with the measured byte size and SHA-256. The command follows the same
  curl-level philosophy as `model acquire`: no host or extension allowlist, no
  credentials, no extraction, assembly or service start — identity freezing
  and Windows archive handling stay with `runtime prepare` and `config create`.
  Failures map to five fixed reasons and never echo the URL. The opt-in
  closed-loop e2e test (`tests/integration/test_closed_loop_download_e2e.py`,
  enabled with `LAB_E2E_CLOSED_LOOP=1`) drives both `model acquire` and
  `engine acquire` real downloads and then the full benchmark chain with
  `--model-source` lineage binding; verified once on macOS arm64.

- A three-platform real-model e2e check (`tests/packaging/ci_model_e2e.py` plus
  the `model-e2e` workflow) drives the full CLI chain — init, device-check,
  platform-specific preparation, an externally started Prism llama-server,
  bind, run, report and verify with `--rerender` — against the 105 MB
  SmolLM2-135M-Instruct Q4_K_M model, then asserts the workspace layout
  contract: eleven files (eight run evidence files under `results/` plus three
  report files), the SVG animation statistics line, and a clean verify exit
  after deleting the whole `bench-ops` operations directory. Preparation
  follows each platform's real path: automatic candidates on macOS, runtime
  prepare plus a runtime receipt on Windows (matching the preflight's
  recommended CPU or CUDA profile), and the manual Linux template with the
  observed environment values frozen. Verified end to end on macOS arm64,
  Windows x64 (CUDA) and Linux x64 physical machines; the engine's
  peg-native chat parser rejecting seed-42 output with an invalid UTF-8 byte
  on the Linux CPU path is worked around with seed 1 and recorded as an
  upstream concern. Linux is not part of the CI matrix: cloud runners have no
  cpufreq sysfs, which the strict Linux environment admission deliberately
  blocks, so Linux is verified on a physical machine (the same e2e script runs
  on any eligible host). The first CI run also exposed and fixed a macOS
  candidate defect: machines reporting no power profile wrote null into
  `conditions.profile` and failed contract validation - the generator now keeps
  the existing "unknown" fallback for None observations.

- The offline report now shows an informational animation-element statistic for SVG
  cases, so operators can see the animation makeup without opening a browser: counts
  of SMIL animation elements (`animate`, `animateTransform`, `animateMotion`, `set`,
  including SVG-namespaced forms) and static text matches of `@keyframes` rules and
  `animation` declarations inside `<style>` text. The counts are computed at report
  rendering time from the single stored model output and displayed in the SVG gallery
  and case details with an explicit informational-only label; they never constitute an
  animation verdict, quality score or eligibility conclusion, and they are never
  written to `index.json` or sealed evidence, leaving `report_format_version` and
  `schema_version` unchanged. When the SVG is not extracted, too large or malformed,
  the counts stay null with a reason instead of masquerading as 0.
- Read-only Windows `memory-check`: bounded process inventory, physical memory
  availability, separate working set/private commit, executable groups and
  conservative system/security/service/agent protection. Unknown ownership never
  recommends termination. The Windows diagnostic has its own kind; existing macOS
  diagnostics remain readable with unchanged field semantics.

### Removed

- Removed the `inferyard host-state migrate` command and the legacy host-state
  retirement mechanism (migration transactions, retirement markers and
  maintenance receipts).
- Legacy `local-ai-benchmark-host` lock/state files from the old tool are now
  completely ignored: they are never read, locked or retired, and InferYard no
  longer provides cross-tool mutual exclusion with the old tool.

### Changed

- The usage guide now forbids browser user data inside the benchmark workspace:
  scoring never uses a browser (the SVG case is unscored syntax-checked display),
  so manual report review opens `report.html` in the operator's regular browser;
  isolation profiles and browser check scripts are dropped, `--user-data-dir`
  must never target the workspace or the delivery tree, and if isolation is
  truly required, user data goes to the system temp directory (prefer the
  browser's guest/incognito window). This tightens the #28 delivery rules after
  operator-written browser scripts placed four Chrome profiles (1,137 files) in
  a benchmark root.
- The usage guide now separates an operations directory (`bench-ops/`) from the
  benchmark workspace (`bench-work/`): preparation outputs (init exports,
  models, preflight, candidates, bound configs, runtime assets), operator
  scripts, logs, screenshots, exports and transfer files live in the
  operations directory from the start, while the workspace holds only runs,
  batches and reports, created automatically by `run`, `batch` and `report`.
  Every preparation command writes to a fresh `--out` directory whose parent
  must already exist, run directories embed their frozen inputs, and
  verify/report never depend on preparation
  directories, so the layout changes no verification semantics. README,
  README.zh-CN, MACOS, WINDOWS, configs/README, installation and CLI-reference
  examples follow the new layout. The operations directory is per-run
  scaffolding: once every run is sealed and the shared report verifies
  cleanly (exit 0), the operator may delete that round's bench-ops content
  entirely — sealed evidence embeds its frozen inputs and nothing reads
  bench-ops afterwards; reruns simply redo preparation, which PID changes
  require anyway. Cleanup is explicit and manual; the CLI never deletes
  files. This is a documentation convention only; existing workspaces are
  not migrated or rewritten.
- `model acquire` accepts explicit total, read and cleanup timeouts, retaining
  1800/180/60 seconds as defaults. New `remote_model_source.v2` records preserve
  the actual acquisition budget; existing v1 records remain read-only.
- Acquisition failures report sanitized stages and transfer progress. The parent
  supervises network inactivity as well as the total deadline. Acquisition's
  direct connection and proxy limitations are now documented.
- New runs use evidence layout 2: immutable frozen inputs, append-only
  observations and prepared request bodies, four sampling/event logs, and a
  manifest. A complete ordinary single run plus its report uses eleven files;
  the default two-bundle scope plus one report uses nineteen. These counts cover
  formal run and report outputs only; preparation checks, operator scripts,
  logs and other workspace artifacts are additional. The core schema
  remains 3. Existing legal schema-3 snapshot runs are read-only; evidence is
  never converted or overwritten. Request durability, dirty-state recovery,
  native clean receipts, stopping rules and score denominators are preserved.
- Report generation and verification avoid repeated member validation, deep
  copies and redundant index parsing while preserving canonical JSON output
  and full source checks. Expanded report filters fit narrow displays.
- `run` preflight no longer builds the unused device recommendation and hardware
  snapshot, no longer spawns `nvidia-smi` on non-CUDA backends, and no longer
  writes `device.preflight.json` or `input-provenance.json`. The identity record
  keeps only the preflight free-disk figure the report reads. Existing evidence
  is read unchanged.
- Live entry points now create a clean host state automatically when acquiring
  the host lock for the first time; no explicit initialization step is needed
  on new machines.
- v0.0.1 host states carrying a `migration` envelope are accepted and the
  envelope is ignored; it disappears on the next state write. Leftover
  `inferyard-host-migration.json` receipt files are treated as unrelated files
  and left untouched.
- README, AGENTS.md, CHANGELOG.md, CONTRIBUTING.md, MACOS.md and WINDOWS.md
  are now written in English; a Chinese README is available at
  `README.zh-CN.md`. All other docs stay Chinese for now.

## 0.0.1

First public distribution. The Python package and CLI are both named
`inferyard`, the license is MIT, and the active data contract remains
`schema_version = 3`.
See [GitHub Releases](https://github.com/sunny0826/inferyard/releases) for the
actual release state and assets.

### First use and legacy data

- Run `inferyard host-state migrate` before the first live operation to
  explicitly initialize the new host state or retire the fixed old entry
  point. Existing dirty state must go through the original recovery flow; it
  cannot be cleared by deleting locks or re-running the migration. (Removed in
  the Unreleased changes above — live entries now initialize automatically.)
- Only the [current format set](docs/contracts/inferyard-current-format.md) is
  accepted. Old runs, old report formats and `origin=migrated` are explicitly
  rejected; old evidence stays with the original project and is not converted
  in InferYard.
- Installation verification uses `installed_safe_checks.v3`, covering current
  report generation/verification and old-report rejection; old installation
  results inherit no qualification.

### Features

- Executes frozen task bundles, saving per-case answers, deterministic
  scoring, client-side timings and platform resource evidence.
- Supports single and batch plans, cancellation and resume, and explicit
  reruns; model services are started externally by the operator.
- Generates self-contained offline HTML reports, plus comparison, rescoring,
  export, redacted public packages and evidence verification.
- Ships task bundles, platform templates and fixed Windows runtime profiles;
  supports `init`, `config assets/create/bind` and `runtime prepare`. Models,
  engines, real configurations and original runs are not distributed with the
  package.
- Supports `uv tool` / `uvx` installation from the same batch of wheels and
  dependency constraints; Windows catalogues and metrics are explicitly read
  as UTF-8.

### Distribution and verification

- GitHub Actions builds wheels/sdists, rebuilds from the sdist and checks the
  resources, then runs installation checks on Linux x64, Windows x64 and
  macOS arm64.
- `community_distribution.v2` candidates bind source, build and installation
  results; after verification the same bytes are uploaded with `SHA256SUMS`.
- The publish workflow is manual-trigger only. `verify-only` runs a release
  rehearsal; `github-release` creates a draft with attachments.
  PyPI uses a separate Trusted Publisher configuration; this version has not
  been uploaded to PyPI.

Installation checks use synthetic inputs; native model preparation, full
bundles and strict performance qualification for the current version are not
proven by these checks.
See [platform status](docs/platforms.md) and the [backlog](docs/backlog.md)
for verified scope and remaining items.
