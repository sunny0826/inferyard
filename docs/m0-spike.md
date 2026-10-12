# T0 / M0 技术验证

日期：2026-10-12。执行者：implementer-A。结果：六项完成条件全部通过。

## 环境与来源

- 实际构建：Go **1.27.1 darwin/arm64**，Bun **1.3.5**，MyGo **v0.4.5**。
- macOS **26.6.2**，WKWebView；浏览器为 Google Chrome。
- 前端依赖：Vite **8.3.4**、TypeScript **7.0.2**、mygo-cli **0.4.5**，版本由 bun.lock 固定。
- 按 [MyGo 官方 getting started](https://mygo.egoist.dev/docs/getting-started) 的 web 模板初始化。
- 只读参考 `../inferyard/docs/plans/studio-app.md`，未修改 inferyard 仓库。

## 下载方式（实际采用）

1. 后续所有 Go 获取命令使用命令级 `GOPROXY=https://goproxy.cn,direct`。
   模板日志确认获取 purego v0.11.1、x/image v0.46.0、typesetting v0.3.5；MyGo v0.4.5 复用前一轮已下载模块。
2. 初始化子进程使用 `npm_config_registry=https://registry.npmmirror.com`；后续显式执行
   `bun install --registry https://registry.npmmirror.com`，项目 `.npmrc` 同样配置镜像。
   bun.lock 中的下载 URL 指向 npmmirror。
3. 本会话原有命令环境包含 `http_proxy=http://127.0.0.1:7897` 和
   `https_proxy=http://127.0.0.1:7897`，下载命令继承了它们。因此实际为**镜像源 + 本机代理环境**；
   未单独验证镜像直连，也没有因镜像失败而切换源。未运行 `go env -w`，未改用户全局配置。
4. 补充要求之前的失败命令使用 proxy.golang.org，继承上述代理：MyGo 模块下载成功，
   自动下载 Go 1.27.2 中途 `unexpected EOF`。本轮改用已安装的 Go 1.27.1，无需下载工具链。
5. 官方文档通过浏览工具读取；其传输代理不可见。未访问其他业务网络。
   本地 HTTP/curl 验证明确绕过代理。

## 六项验收

| 条件 | 结果 | 证据 |
| --- | --- | --- |
| 1. 桌面窗口加载本地 HTTP 页面 | 通过 | [desktop.log](evidence/desktop.log)：端口 62938，`desktop page loaded`；[桌面截图](evidence/desktop.png) |
| 2. 浏览器/curl 同地址同端口可达 | 通过 | [http.log](evidence/http.log)：HTTP 200、curl 退出 0；[browser.txt](evidence/browser.txt)：Chrome 同端口可见页面 |
| 3. SSE 双端到达 | 通过 | [sse.log](evidence/sse.log)：31→32→33；desktop.log 中 DOM 2→4；browser.txt 中页面 53→63；三客户端在同一桌面进程存活期间连接 |
| 4. embed 单二进制服务前端 | 通过 | [embed.log](evidence/embed.log)：临时移走 web/dist，在 build/ 中启动二进制，首页、JS、CSS 均 200；全程未启动 Vite dev server |
| 5. 三平台 go build 退出 0 | 通过 | [build.log](evidence/build.log)：darwin/arm64、windows/amd64、linux/amd64 全部退出 0 |
| 6. 完整记录 | 通过 | 本文件包含版本、命令、下载方式、逐项证据、问题与未验证项 |

## 关键命令及退出码

本机命令前缀 `GOENV` 在下表表示以下命令级环境，不是全局设置：

```sh
env -u GOROOT PATH=/Users/guoxudong/.local/share/mise/installs/go/1.27.1/bin:$PATH GOPROXY=https://goproxy.cn,direct
```

| 实际命令（前缀简写） | 退出码 | 说明 |
| --- | --- | --- |
| `go version` / `bun --version`（初始 PATH） | 0 / 0 | 初始 Go 为 1.24.2；Bun 为 1.3.5 |
| `go run github.com/egoist/mygo/cmd/mygo@latest doctor`（补充要求前） | 1 | 自动下载 Go 1.27.2 时 unexpected EOF |
| `GOPROXY=https://goproxy.cn,direct <Go 1.27.1绝对路径> run github.com/egoist/mygo/cmd/mygo@latest doctor` | 1 | 继承 GOROOT 指向 1.24.2，缺少 encoding/json/v2 |
| `GOENV go run github.com/egoist/mygo/cmd/mygo@latest doctor` | 0 | [doctor.txt](evidence/doctor.txt) 为命令输出转录 |
| `GOENV npm_config_registry=https://registry.npmmirror.com go run github.com/egoist/mygo/cmd/mygo@v0.4.5 init -template web -name 'InferYard Studio' -module inferyard-studio .scaffold` | 0 | 仓库内空子目录生成后移入根目录，保留 .git |
| `bun install --registry https://registry.npmmirror.com` | 0 | 使用项目镜像，移除不使用的 mygo-runtime |
| `bun run build:web` | 0 | [frontend-build.log](evidence/frontend-build.log)，含 TS 检查 |
| `GOENV sh scripts/build-all.sh` | 0 | 三次 go build 各自退出 0；CGO_ENABLED=0 |
| `GOENV go test -race ./...` | 0 | [tests.log](evidence/tests.log)，真实 HTTP 鉴权、随机 token、双 SSE 客户端接收相同递增事件 |
| `./build/darwin-arm64/inferyard-studio -spike-evidence docs/evidence` | 0 | 真实桌面验证完成后 SIGINT 正常退出 |
| `curl --noproxy '*' -sS -o docs/evidence/page.html -w 'HTTP %{http_code}\n' "$URL"` | 0 | 同桌面端口，URL 来自当前进程日志 |
| `curl --noproxy '*' -N -sS --max-time 7 "$EVENTS_URL"` | 28 | 持续流被主动限制为 7 秒，收到三条事件；28 是预期超时，不是连接失败 |
| `python3 scripts/verify-embed.py` | 0 | 无 dist 目录时验证 embed；headless 子进程退出 0；finally 恢复目录 |

`file` 检查三产物分别为 Mach-O arm64、PE32+ x86-64、ELF x86-64。
构建产物位于 `build/<os>-<arch>/`，不提交二进制和 node_modules。
日志中的会话 token 已脱敏，原始日志被 .gitignore 排除。

## 实现与复跑

- `main.go` 使用 `net.Listen("tcp", "127.0.0.1:0")`，每个进程生成 32 字节随机 token。
- 首页 query token；SSE query token；演示 REST `/api/status` 的 `Authorization` header 直接传 token。
  不正确或缺失 token 返回 401。`/assets/` 只提供公开构建静态文件，不包含会话数据。
- 标准库 SSE hub 每 2 秒广播同一递增计数；没有 WebSocket、mygo.Bind 或第三方 HTTP 包。
- Vite/TypeScript 前端移至 web/；web/assets.go 用 embed.FS 嵌入 web/dist。
- `bun run dev` 先构建前端再 go run；`bun run build:all` 生成三个二进制。
- `-headless` 可只启动 HTTP；`-spike-evidence` 仅用于本阶段 DOM 采样和截图。
- 浏览器使用日志中完整 URL；关闭桌面窗口或 Ctrl+C 会停止该会话。

## 问题与处理

1. PATH 与 GOROOT 指向旧 Go。找到 mise 已安装的 1.27.1，命令级切换 PATH 并清除 GOROOT 后解决。
2. 官方 init 要求空目录。使用仓库内 .scaffold，完成后移入根目录，未删除 .git。
3. 空 Git 仓库使 init 的临时绑定构建报告 `git log` 无提交；init 本身退出 0。
   实际项目不使用桥接绑定，已删除生成的 src/mygo.ts；最终三个 go build 均通过。
4. 模板首页仍含示例 Greeter DOM，替换为 #app 后重新构建全部产物，再进行运行验收。
5. 浏览器扩展接口加载 request-header policy 失败；改用 cua_repl 的 Chrome 原生辅助功能树，确认页面与计数增长。
6. doctor 提示缺少可选 Windows 安装器／签名工具，本任务不打安装包、不签名，不构成阻塞。

## 未验证与边界

- Windows、Linux 仅验证交叉编译，未在真实系统运行 GUI；系统 WebView/GTK 动态依赖未验收。
- 不包含签名、dmg、installer、发布、热更新和真实 inferyard CLI 集成。
- M0 粗粒度鉴权已验证，不代表后续完整 API 安全验收。
- 当前无阻塞项。测试桌面进程和浏览器标签页已关闭。
