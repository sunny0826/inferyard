# InferYard Studio

M0：MyGo 桌面窗口与浏览器共用一个本地 HTTP 服务，展示每两秒递增的 SSE 计数。

需要 Go ≥ 1.27.1、Bun。Go 的 PATH 与 GOROOT 必须指向同一工具链。

```sh
bun install --registry https://registry.npmmirror.com
GOPROXY=https://goproxy.cn,direct bun run build
./build/inferyard-studio
```

浏览器打开日志中的完整 `STUDIO_URL`（包含本次会话 token）。关闭桌面窗口会退出服务。
只启动 Web 服务：`./build/inferyard-studio -headless`，按 Ctrl+C 退出。

```sh
GOPROXY=https://goproxy.cn,direct bun run dev
GOPROXY=https://goproxy.cn,direct bun run build:all
GOPROXY=https://goproxy.cn,direct go test -race ./...
python3 scripts/verify-embed.py
```

`dev` 先构建前端，再运行 Go；M0 不启动 Vite dev server，也不提供热更新。
`build:all` 只生成三平台二进制，不生成安装包。运行时需要操作系统的 WebView。

- `main.go`：随机本地端口、生命周期、MyGo 窗口。
- `internal/server/`：token 校验、静态文件、SSE 广播。
- `web/`：Vite + TypeScript、Go embed；构建前须生成 `web/dist`。
- `scripts/`：交叉构建与独立二进制验证。
- [M0 验证记录](docs/m0-spike.md)：命令、退出码、证据和限制。

首页使用 query token，`/api/events` 使用 query token，`/api/status` 使用
`Authorization: <token>`。静态 `/assets/` 文件公开，不包含会话数据。
