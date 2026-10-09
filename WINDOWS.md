# Windows Guide

The target is Windows x64. The formal serial text entry points support Prism,
KVMem and NInfer; the latter two are limited to fixed-quality plans.
Implementation and historical native coverage are tracked in
[platform status](docs/platforms.md).

## Device preflight and single runs

After installing uv and the local wheel, use the installed `inferyard` in
PowerShell; no repository scripts or D: drive are required.
Full CPU/CUDA archive preparation is in the
[installation guide](docs/installation.md#windows-x64先准备固定-runtime).

```powershell
inferyard init --out bench-work --bundle zh-smoke
inferyard device-check --model 'C:\Models\model.gguf' --out 'bench-work\preflight' --json
inferyard runtime prepare --profile prism-b10743-adfffbe-win-cpu-x64 `
  --archive 'C:\Downloads\llama-prism-b10743-adfffbe-bin-win-cpu-x64.zip' `
  --out 'bench-work\assets\runtime'
inferyard config create --preflight 'bench-work\preflight\device-preflight.json' `
  --runtime-receipt 'bench-work\assets\runtime\runtime-receipt.json' `
  --bundle 'bench-work\bundles\zh-smoke.json' --results 'bench-work\results' `
  --out 'bench-work\candidate'
```

The example picks the CPU profile, which must match the mode recommended by
the preflight; CUDA requires the matching profile and runtime archive.
The preparation commands do not download or start services; `runtime prepare`
and `config create` run `--version` of the verified engine.
The candidate references the EXE/DLL and manifest in the runtime directory
without copying them; recreate the candidate after moving assets.

Start the service in another terminal with the candidate's full
`engine.startup_args`, confirm the actually listening PID, then bind:

```powershell
inferyard config bind --candidate 'bench-work\candidate\candidate.toml' `
  --pid 1234 --endpoint http://127.0.0.1:48857 --out 'bench-work\bound'
inferyard run --config 'bench-work\bound\config.toml'
inferyard report --runs 'bench-work\results\RUN_ID' --out 'bench-work\report'
inferyard verify --path 'bench-work\report'
```

`RUN_ID` comes from the run output. Quote paths containing spaces; replace
`1234` with the actual PID and never assign to PowerShell's read-only `$PID`
variable.
KVMem/NInfer require the asset manifest and arguments prepared per the
[configuration README](configs/README.md#kvmem--ninfer), then use the same
`config bind` entry point.

## Sampling and support boundaries

Identity verification uses same-account processes, full creation FILETIME,
EXE/DLL, argv/cwd, model files and a unique loopback listener.
A numeric PID, a successful service `/health` or the presence of a GPU device
cannot substitute for these checks.

Prism's `windows-resource.v1` and KVMem/NInfer's `windows-memory.v1` sample
RSS and host available memory; temperature, frequency, power, energy and
other unsampled fields keep null plus a reason.
WMI `AdapterRAM` is never used to prove large VRAM, ACPI thermal zones are
not CPU package temperature, and commit charge is not paging counts.
Linux governor/EPP stays unknown without a matching source.

Windows-only entry points such as live overhead, live extensions and
prepare-length remain restricted.
KVMem/NInfer `auto` mode can use native OpenAI generation signals, but does
not infer exact template budgets, effective arguments or engine-internal
draining.
Explicit `lab_required` demands the full lab protocol; see the
[adaptation contract](docs/contracts/windows-ninfer-adaptation-contract.md).

## Locks, persistence and offline evidence

The first live entry creates a clean host state automatically when it
acquires the host lock; no explicit initialization command is needed. The
lock and state are `inferyard-host.lock` and `inferyard-host.state.json`
inside the native `CSIDL_COMMON_APPDATA` (usually `C:\ProgramData`).
The old tool's `local-ai-benchmark-host.*` files (including the old D-drive
locations) are neither read nor locked; leftover
`inferyard-host-migration.json` receipt files are left untouched. Lock
contention, state conflicts and permission errors still block.
Files reject reparse points and keep file fsync plus
MoveFileExW(WRITE_THROUGH); directory fsync is unavailable.
Windows-native ACL, volume changes and persistence are not yet verified;
temporary-directory simulations and Go cross-builds do not count as native
evidence. Known boundaries are in the
[current format contract](docs/contracts/inferyard-current-format.md).

`report`, `compare` and `verify` work offline; only current formats are
accepted, and old evidence stays with the original project.
Derived artifacts are written to new directories; a format rejection must
not be reported as evidence-integrity success.

## Windows engine-fit diagnostics

`engine-fit run` supports single-GGUF llama.cpp only, using the separate
run.v6; other Windows engines are rejected before any request.
Planning, external services and the full commands are in the
[engine guide](docs/engine-fit-common.md).
It records the native process-tree working set/CPU seconds, host memory and
available NVIDIA temperatures; CPU temperature is a missing measurement.
These diagnostics are separate from formal quality runs; see the
[Windows engine-fit contract](docs/contracts/engine-fit-contract.md#windows-原生来源).
Source development uses the mise/uv commands in the
[contributing guide](CONTRIBUTING.md).
