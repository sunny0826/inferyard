# Checkout development tools

Run these commands from the repository root with the tools pinned by
[mise.toml](../mise.toml):

```sh
mise run dev
mise run dev:rebuild
mise run dev:stop
```

`dev` builds the native Studio and a CLI wheel from the current checkout, including
uncommitted source changes. It installs the local wheel and locked runtime dependencies
into a private virtual environment, checks the resulting pair, then opens the real
Studio window. It preserves this branch's development workspace. Run it again after
editing source; both running programs keep the version built at startup.

`dev:rebuild` also creates fresh settings, window layout and workspace. Previous runs,
reports, logs, build records and downloaded model/engine assets remain on disk. It does
not clear uv/Go caches or change the normal CLI virtual environment. All generated
development data stays in the ignored `.inferyard-dev/` directory of this checkout.
Different branches and worktrees use separate profiles.
On macOS, the native executable is wrapped in `InferYard Studio Dev.app` with a
separate application identifier, so the desktop can distinguish it from the release.

Preparing a pair may download locked Python/Go dependencies through the existing
package managers. It initializes a new workspace, but does not download models or
engines, start a model service, or run a benchmark. Cached assets still pass the normal
CLI identity and eligibility checks before use.

The new pair is prepared before the recorded development instance is asked to close.
Studio cancels and drains work, then stops its own engine. A shutdown error prevents
switching environments; inspect the reported session and log paths. The launcher only
controls its own recorded instance and retains the real host lock and dirty-state
recovery rules. `dev:stop` requests the same graceful shutdown without building.

The window is labelled as a development environment. Its CLI and directory bindings
are fixed for that launch. Normal Studio settings, PATH discovery and release-package
installation cannot replace the prepared CLI. Download directory names remain editable.
The CLI's `source=release` describes its non-editable installation; the development
manifest records the checkout, source digest and local wheel separately.

Build and validate without opening a window or switching the current profile:

```sh
mise run dev -- --prepare-only
```

That command still creates new build/workspace evidence. It does not prove native UI
behavior. This workflow requires a logged-in desktop for the normal `dev` commands.

If a previous Studio exited before writing its session receipt, `dev` and `dev:rebuild`
use the newly prepared Studio to check that old launch. Recovery requires its original
manifest and PID record, a natively absent child and no engine ownership record.
Unknown or live processes, damaged records and failed shutdowns block the switch.
Original builds, manifests and logs are preserved. A valid session is always stopped
by its original Studio, with the existing process identity checks.

To recover an older missing-session launch without opening another window, prepare
a controller and pass the absolute manifest path printed by that command:

```sh
mise run dev -- --prepare-only
mise run dev:stop -- --controller-manifest /absolute/printed/manifest.json
```

`--controller-manifest` is valid only for `stop`. Stop does not build or run the CLI;
without an explicit controller it requires the original session receipt. Do not edit
PID records, remove owner/host locks or replace old binaries to force recovery.

It is separate from Studio's simulated `--debug-ui` preview. Development switches are
compiled only with `studio_dev` and excluded by `release`.
Launch the development executable through the mise task. Opening its `.app` or
binary without a launch manifest is rejected instead of loading normal user settings.

Checks from the repository root:

```sh
mise exec -- python -m unittest discover -s tools/tests -v
```

The [development subsystem reference](../docs/subsystems/studio-development.md) owns
the runtime contract and failure behavior. Native checks remain documented in the
[Studio README](../studio/README.md#verification-boundaries).
