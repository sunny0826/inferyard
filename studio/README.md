<p align="left">
  <img src="resources/icon.png" alt="InferYard Studio icon" width="76" height="76">
</p>

# InferYard Studio

InferYard Studio is a native Go (`1.27.3`) desktop app built on [onevra](https://github.com/oneo-me/onevra) UI components and MyGo `v0.10.0`. Pages call `core.Service` in-process and invoke the [CLI subproject](../cli/README.md) (`inferyard`) for benchmark execution and `studio.json` report generation.

## Pages and first launch

On macOS, open the downloaded `.dmg` from [Releases](https://github.com/sunny0826/inferyard/releases/latest), drag `InferYard Studio.app` into `Applications`, and clear the quarantine attribute before first launch (the release build is unsigned):

```sh
xattr -dr com.apple.quarantine /Applications/InferYard\ Studio.app
```

1. Open **Tasks** and choose **Prepare Environment** to set up or reuse `uv` (`>= 0.13.6`), `inferyard`, and workspace directories. When a newer CLI is available, Studio detects the version mismatch and offers one-click **Upgrade CLI**.
2. If local assets are missing, open **Downloads** to follow the hardware-aware starter recommendation (one-click managed **PolyStrata** setup with 4-stage progress/recovery on supported Apple Silicon, NVIDIA CUDA, or AMD ROCm hardware, or switch to lightweight `llama.cpp` + GGUF), browse the engine/model catalog, or configure an existing local engine from **Models**.
3. On **Tasks**, select an engine, model, and task sets (`zh-core` + `zh-svg-pelican` by default, or switch to **Long-Context Speed** / `zh-context-speed` for 64K / 128K / 256K), then click **Start Benchmark**. Studio manages local services it launches and stops them on exit, or binds a running local service after PID/identity checks.
4. Click **View Report** when finished, or navigate between the six views in the sidebar:

| View | What it provides |
| --- | --- |
| **Tasks** | Environment setup & one-click **Upgrade CLI**, **New Task** launcher for standard bundles & **Long-Context Speed** (`zh-context-speed`: 64K / 128K / 256K), local service lifecycle & PID-verified binding, engine status card with direct Web UI link, live **Resource Curves**, in-app notification inbox & OS notifications |
| **Reports** | Model-grouped `studio.json` **Benchmark Reports**, quality/latency/throughput/resource breakdowns, **Long-Context Speed** results, and **Run History** table |
| **Models** | Multi-root model directory management, local engine setup wizard (**Models → Configure local engine**), and safe uninstall & disk cleanup for managed PolyStrata installations and downloaded models |
| **Downloads** | Hardware-aware starter recommendations (managed PolyStrata with 4-stage setup/recovery or `llama.cpp` fallback), extensible multi-engine catalog (`PolyStrata`, `llama.cpp`, `mlx-lm`, `vLLM`, `SGLang`, `LM Studio`), Hugging Face & ModelScope mirrors, custom URL resolution (`model resolve`), multi-shard GGUF and directory model downloads with streaming progress and resume |
| **Device** | Read-only hardware readiness inspection (`device-check`), CPU/GPU/memory summary, and starter recommendation details |
| **Settings** | **Appearance** (`Light` / `Dark` / `Follow System` with live visual previews), **Language** (`Follow System` / `English` / `简体中文`), **System Tray** (minimize to tray on window close to keep background runs/downloads active), **Paths**, **Existing Local Engines**, **Download Directory Names**, **Network & Proxy** (scoped HTTP(S) download proxy), and **Software Update** (automatic GitHub Releases check & one-click SHA-256 verified self-update) |

## Internationalization (i18n)

Studio ships with full **English** (`en`) and **Simplified Chinese** (`zh-Hans`) catalogs powered by `mygo/i18n` and embedded JSON resources in `studio/internal/l10n`:

- **Live switching:** Open **Settings → Language** and choose **Follow System**, **English**, or **简体中文**. Labels, navigation, notifications, and tray items update immediately without restarting Studio.
- **Environment override:** Set `MYGO_LOCALE=en` or `MYGO_LOCALE=zh-Hans` when launching Studio to override the resolved locale.
- **UI debug language preview:** `--debug-ui` mode supports switching languages live alongside scenario presets.
- **macOS bundle localization:** Packaged `.app` bundles include `en.lproj` and `zh-Hans.lproj` (`CFBundleLocalizations`) so macOS system menus and dialogs follow the selected locale.

## Build and run

For paired checkout development (builds both CLI and Studio into an isolated `.inferyard-dev/` profile), run from the repository root (see [tools/README.md](../tools/README.md)):

```sh
mise run dev
```

To build Studio directly from `studio/`:

```sh
mise exec -- env GOTOOLCHAIN=go1.27.3 GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0 go build -o build/inferyard-studio .
./build/inferyard-studio
```

To cross-build unsigned distributions (`darwin-arm64`, `windows-amd64`, `linux-amd64`) into `dist/`:

```sh
mise exec -- env GOTOOLCHAIN=go1.27.3 GOPROXY=https://goproxy.cn,direct sh scripts/build-all.sh
```

Formal release artifacts (`.dmg`, Inno Setup `.exe`, `.tar.gz`) are built natively with `-tags release` by [release.yml](../.github/workflows/release.yml). App version metadata lives in [mygo.config.ts](mygo.config.ts). Settings are stored under `InferYard Studio/settings.json` in the OS user configuration directory.

## UI debug preview

Open simulated native pages without touching `core.Service`, user settings, or model processes (available only in `ui_debug && !release` builds; formal release builds reject `--debug-ui`):

```sh
mise exec -- go run -tags ui_debug . --debug-ui
```

Use the sidebar scenario selector to jump to a starting state, switch UI languages live, or drive workflows end-to-end through normal page controls.

## Verification

Preferred root-level `mise` tasks (run from the repository root):

```sh
mise run test:studio          # Studio Go unit tests (macOS)
mise run test:studio:preview  # UI debug preview Go tests (macOS)
mise run test:studio:e2e      # Boundary E2E with synced CLI (cross-platform)
mise run test:studio-ui       # macOS native UI E2E: daily 3 workflows
mise run test:studio-ui:full  # macOS native UI E2E: all 7 workflows
```

Additional direct checks from `studio/`:

```sh
mise exec -- env GOTOOLCHAIN=go1.27.3 GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0 go test -tags ui_debug,release . -count=1
mise exec -- python -m unittest discover -s scripts -p 'test_*.py'
mise exec -- python -m unittest discover -s tests/ui -p 'test_*.py'
```

- Boundary E2E (`tests/e2e`) runs the real CLI against a fake engine; see [tests/e2e/README.md](tests/e2e/README.md).
- macOS native window E2E drives XCUITest against `--debug-ui`; see [tests/ui/README.md](tests/ui/README.md).
