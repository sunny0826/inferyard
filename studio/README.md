# InferYard Studio

InferYard 的本地桌面界面：设备检查、运行队列与取消、历史证据、资源曲线、报告和模型下载。
Go 服务调用本机 `inferyard` CLI，MyGo 窗口加载随机 `127.0.0.1` 端口上的嵌入式前端。
在设置页配置 CLI 路径、结果目录和模型目录；未配置 CLI 时可以启动界面。

## 构建与运行

需要 Go（本项目验收使用 `go1.27.2`）、Bun；桌面运行还需要系统 WebView。
先构建前端，再构建 Go 二进制，产物内含 `web/dist`：

```sh
bun install --registry=https://registry.npmmirror.com
bun run build:web
env GOTOOLCHAIN=go1.27.2 GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0 go build -o build/inferyard-studio .
./build/inferyard-studio
```

开发启动：`env GOPROXY=https://goproxy.cn,direct bun run dev`，先构建前端再运行 Go。
`bun run dev:mock` 使用前端模拟数据，不连接真实 CLI。
三平台二进制构建：`env GOTOOLCHAIN=go1.27.2 GOPROXY=https://goproxy.cn,direct bun run build:all`。
二进制位于 `build/{darwin-arm64,windows-amd64,linux-amd64}/`；分发目录位于
`dist/inferyard-studio-v0.1.0-<os>-<arch>/`，包含二进制和运行说明。
macOS 另含 `InferYard Studio.app`，通过 `open "InferYard Studio.app"` 启动以使用系统通知。
未执行签名、notarization 或发布。

桌面模式订阅 EventBus，在运行和下载进入 done/failed/cancelled、报告生成完成时通知。
通知仅含固定标题和一句正文，不含任务路径。首次使用可能请求系统通知权限；
拒绝权限或通知失败仅记录日志，不影响任务。Headless 不订阅通知。
MyGo API 参考：https://mygo.egoist.dev/docs/native#notifications 。

版本号唯一来源为 `mygo.config.ts` 的 `version`。`main.go` 通过 `go:embed` 将配置嵌入二进制，
启动时设置后端 `/api/status` 的版本；普通 `go build` 也生效，无须生成或传入 ldflags。
修改版本后重新构建。单独使用 core 包时版本为 `dev`。

## Headless 与会话鉴权

`-headless` 只启动本地服务，按 Ctrl+C 退出。日志只含监听地址，**token 和完整会话 URL 永不落日志**。
外部启动器须创建私密管道，将写端文件描述符（≥3）传入 `-session-fd`；服务写一次 URL 后关闭写端。
例如 macOS/Linux 上通过匿名管道取得 URL 并直接打开浏览器：

```sh
python3 - <<'PY'
import os
import signal
import subprocess
import webbrowser

reader, writer = os.pipe()
process = subprocess.Popen(
    ['./build/inferyard-studio', '-headless', '-session-fd', str(writer)],
    pass_fds=(writer,), start_new_session=True,
)
os.close(writer)
try:
    with os.fdopen(reader) as pipe:
        url = pipe.readline().strip()
    if not url:
        raise RuntimeError('Server did not publish a session URL')
    webbrowser.open(url)
    process.wait()
except KeyboardInterrupt:
    pass
finally:
    if process.poll() is None:
        process.send_signal(signal.SIGINT)
        process.wait()
PY
```

REST API 使用 `Authorization: Bearer <token>`。入口页、报告页和 `/api/events`（EventSource）
也接受 `?token=<token>`；其他 REST 路由不接受 query token。静态 `/assets/` 无需鉴权。
SSE 推送运行、进度、资源、下载、设备及报告事件；`sync.reset` 后客户端通过 REST 重建状态。

设置保存在系统用户配置目录的 `InferYard Studio/settings.json`，目录权限 0700、文件权限 0600
（POSIX）。损坏 JSON 或字段类型错误时使用默认设置并记录提示；用户保存后才替换文件。
历史扫描跳过损坏 run，并在服务端日志记录跳过数量。

## 验证

以下环境变量仅作用于对应命令，不修改全局配置：

```sh
env GOTOOLCHAIN=go1.27.2 GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0 go vet ./...
env GOTOOLCHAIN=go1.27.2 GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0 go test ./...
env GOTOOLCHAIN=go1.27.2 GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0 go test -race ./...
env GOTOOLCHAIN=go1.27.2 GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o /tmp/inferyard-studio-windows.exe .
env GOTOOLCHAIN=go1.27.2 GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /tmp/inferyard-studio-linux .
python3 scripts/verify-embed.py --binary build/inferyard-studio
```

嵌入验证会临时移走 `web/dist` 并自动恢复，请勿与前端构建并行执行。
它从空目录运行二进制，检查页面与资产、鉴权、token 不落日志及正常退出。
交叉编译不等于 Windows/Linux 上的运行验证，fixture 测试也不等于真实模型验收。

- `internal/core/`：任务、设置、报告和证据聚合。
- `internal/evidence/`：只读索引和 JSONL 增量读取。
- `internal/inferyard/`：CLI 子进程和结构化结果解析。
- `internal/server/`：HTTP、鉴权和 SSE；[端点与验证说明](internal/server/README.md)。
- `web/`：Svelte 前端与嵌入资产。

## First-run onboarding

Dashboard includes a non-blocking readiness card and a workspace-driven wizard. Settings adds a
workspace directory, with `~/.inferyard` as its initial effective value. Preparing directories is an
explicit, idempotent action; saving settings alone does not create them.

The wizard accepts model and engine URLs, follows `progress.v1` through both default bundles, shows
frozen service arguments for the operator to copy, and lists local process IDs for binding. Model
credentials are supplied only by an environment-variable name. Studio never starts or stops the
model service. Runs use the existing queue, cancellation and notification behavior. The final action
creates a report and verifies it.

`init` requires a new directory. If the selected workspace already exists without initialization,
the wizard suggests an editable new child directory and switches to it after successful initialization.
Existing files are retained. This directory handoff and the real CLI workflow still require integration
verification. `bun run dev:mock` demonstrates the workflow without downloads or model requests;
`bun web/tests/verify.mjs` covers both the mock walkthrough and HTTP/SSE fixtures.
