<p align="left">
  <img src="resources/icon.png" alt="InferYard Studio icon" width="76" height="76">
</p>

# InferYard Studio

InferYard Studio is a native Go desktop app built on [onevra](https://github.com/oneo-me/onevra) and MyGo v0.7.4. Pages call `core.Service` in-process and invoke the [CLI subproject](../cli/README.md) (`inferyard`) for benchmark execution and `studio.json` report generation.

## Pages and first launch

On macOS, open the downloaded `.dmg` from [Releases](https://github.com/sunny0826/inferyard/releases/latest), drag `InferYard Studio.app` into `Applications`, and clear the quarantine attribute before first launch (the release build is unsigned):

```sh
xattr -dr com.apple.quarantine /Applications/InferYard\ Studio.app
```

1. Open **任务** (Tasks) and choose **准备环境** (Prepare environment) to set up or reuse `uv`, `inferyard`, and workspace directories. If local assets are missing, open **下载** (Downloads) to use the hardware-aware starter recommendation (one-click managed **PolyStrata** setup on supported Apple Silicon, NVIDIA CUDA, or AMD ROCm hardware, or switch to lightweight `llama.cpp` + GGUF) or register an existing local engine.
2. On **任务**, choose an engine, model, and task sets (`zh-core` + `zh-svg-pelican` by default, or switch to **长上下文速度**), then click **开始测评**. Studio manages local services it starts and stops them on exit, or binds a running local service after PID/identity checks.
3. Click **查看报告** when finished, or browse **报告** (Reports), **模型** (Models), **下载** (Downloads), **设备** (Device), and **设置** (Settings) from the sidebar.

## Build and run

For paired checkout development (builds both CLI and Studio into an isolated `.inferyard-dev/` profile), run `mise run dev` from the repository root (see [tools/README.md](../tools/README.md)).

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

Open simulated native pages without touching `core.Service`, user settings, or model processes (available only in `ui_debug && !release` builds):

```sh
mise exec -- go run -tags ui_debug . --debug-ui
```

Use the sidebar **模拟场景** selector to jump to a starting state, or drive workflows end-to-end through normal page controls.

## Verification boundaries

From `studio/`:

```sh
mise exec -- env GOTOOLCHAIN=go1.27.3 GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0 go test ./...
mise exec -- env GOTOOLCHAIN=go1.27.3 GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0 go test -tags ui_debug . ./internal/pages -count=1
mise exec -- env GOTOOLCHAIN=go1.27.3 GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0 go test -tags ui_debug,release . -count=1
mise exec -- python -m unittest discover -s scripts -p 'test_*.py'
mise exec -- python -m unittest discover -s tests/ui -p 'test_*.py'
```

- Boundary E2E (`tests/e2e`) runs the real CLI against a fake engine; see [tests/e2e/README.md](tests/e2e/README.md).
- macOS native window E2E (`mise run test:studio-ui` / `mise run test:studio-ui:full`) drives XCUITest against `--debug-ui`; see [tests/ui/README.md](tests/ui/README.md).
