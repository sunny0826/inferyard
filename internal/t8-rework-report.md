# T8 后端返工报告

基线：`e8cb5b6`（main）。只修改 main.go、internal/** 和根 README.md。
无新增依赖；未修改 web/**、契约或 mygo.config.ts；未 push。
提交哈希在交付消息中提供。本报告对应本次后端提交。

## 修复结果

| 项目 | 结果 |
| --- | --- |
| B1 | `RunStatus` 只将 `user_cancelled` 判为 cancelled，保持 cancelled > dirty > partial > complete。删除 `interrupted` / `cancelled` 两个伪造 reason 判据。 |
| S1 | 相邻 500ms 窗口内、同 clock/phase 的不同 metric 合并为一个采样点；重复 metric 开始新周期。跨 Tail 读取保留合并状态，REST 缓存和 SSE 待发点按首条 UTC 更新完整点；新周期缺失字段保持 null。CPU 差分条件不变。 |
| S2 | `schema_version > 1` 不匹配 dirty；小于 1、缺失或非法 schema，以及非法 v1 字段仍报错。 |
| S3 | 损坏 inputs/manifest/batch 条目计数跳过，损坏 events 对应 run 也跳过，日志记录总数。正常 standalone/batch run 保留，响应形状不变。批次 summaries 与有效 runs 同步过滤，防止 resume 索引错位。索引注释区分局部损坏与遍历失败。 |
| S5 | 根 README 更新为现行启动、构建、私密 session-fd 管道、Bearer/query token 路由范围、验证及版本来源说明。 |
| S6 | 嵌入资产实测：JS 230232、CSS 5813、HTML 439 字节，更新 server README。数值来自本轮并行前端构建后的二进制。 |
| S7 | `mygo.config.ts` 是唯一版本来源。main.go 用 go:embed 嵌入并读取其静态 version 属性，启动服务前设置 core.Version；普通 go build 生效，无生成步骤或额外构建参数。core 包单独使用时为 dev。 |
| S8 | 持久化设置前，对已存在的设置目录执行 chmod 0700；原子替换文件保持 0600。 |
| S9 | JSON 损坏或字段类型错误时重置全部设置为默认值，日志提示，不因解析失败中断启动；不保留部分成功解析字段，不在启动时覆盖原文件，后续保存可恢复。 |

本轮指定项目全部完成。S4 属并行前端任务；S10–S12 未列入本轮后端返工范围。

## B1 根因与 diff 要点

归档审查链已明确：trial_runner 的用户取消返回 `(130, "user_cancelled")`，
trial_lifecycle 将该 reason 写入 `run_stopped`。历史扫描不接收进程退出码，原实现却匹配
CLI status/伪造 reason，导致真实取消一律退化为 partial 或 dirty。
修复位于 `evidence/dirty.go`；`evidence/dirty_test.go` 覆盖真实 reason，
`server/rework_test.go` 另验证取消且 dirty 的 run 经 GET /api/runs 仍为 cancelled。
预算、安全、固定时窗中断、诊断、check-only、tool_interrupted 均不判取消。
缺 reason 时仍根据 dirty、sealed 和 completeness 派生。

## 验收命令和结果

以下 Go 命令均添加命令级前缀：

```sh
env GOTOOLCHAIN=go1.27.2 GOPROXY=https://goproxy.cn,direct CGO_ENABLED=0
```

定向测试命令 T（以下表格各行引用同一次实际执行）：

```sh
go test ./internal/evidence ./internal/core ./internal/server -run 'Test(RunStatusRealStopReasons|DirtyRunSchemaVersions|ResourceSamplingWindow|ResourceCPUIdentityAndMissing|CorruptSettingsUseDefaults|SettingsExistingDirectoryPermissions|HistorySkipsCorruptRuns|Index)' -v
```

| 项目 | 命令 | 退出码 | 输出要点 |
| --- | --- | --- | --- |
| B1 | T：TestRunStatusRealStopReasons、TestHistorySkipsCorruptRuns | 0 | user_cancelled 优先于 dirty；7 个非取消真实原因保持 partial/dirty；无 reason 的派生通过；HTTP 返回 cancelled。 |
| S1 | T：TestResourceSamplingWindowMerge/Boundaries、TestResourceCPUIdentityAndMissing | 0 | 8 条 metric 合成 2 点；跨秒边界与逐条读取结果一致；CPU 为 null/50%；下一周期不继承缺失值；间隔/重复字段开启新点。 |
| S2 | T：TestDirtyRunSchemaVersions | 0 | future/current/zero/negative/absent/invalid_version/fraction/invalid_dirty/missing_dirty/missing_identity/malformed 全通过。 |
| S3 | T：TestHistorySkipsCorruptRuns、TestIndex/SealAndIdentity | 0 | HTTP 200；4 个损坏 run 被跳过并记录 skipped 4 entries；2 个 standalone 和 1 个 batch run 保留；顶层仍为原 4 字段。 |
| S8 | T：TestSettingsExistingDirectoryPermissions | 0 | 已有 0755 目录保存后变 0700；文件 0600。 |
| S9 | T：TestCorruptSettingsUseDefaults | 0 | 损坏 JSON、部分合法字段加类型错误、数组均恢复默认并提示；原文件保留；可重新保存。 |
| 全量静态检查 | go vet ./... | 0 | 无诊断。 |
| 全量测试 | go test ./... | 0 | 79 passed in 6 packages。 |
| Race | go test -race ./... | 0 | 79 passed in 6 packages，无 race。 |
| Windows | GOOS=windows GOARCH=amd64 go build -o /tmp/inferyard-studio-t8-windows.exe . | 0 | 无编译错误，生成二进制。 |
| Linux | GOOS=linux GOARCH=amd64 go build -o /tmp/inferyard-studio-t8-linux . | 0 | 无编译错误，生成二进制。 |
| macOS 原生 | go build -o /tmp/inferyard-studio-t8-darwin . | 0 | 生成下述 headless 实测二进制。 |
| S5/S6 文档核对 | python3 -（检查 README 必备指引及 web/dist 文件大小） | 0 | 旧 STUDIO_URL/SSE 计数/裸 token 指引消失；新启动/鉴权/构建/验证/版本说明齐全；资产 230232/5813/439。 |
| S6/S7 进程实测 | python3 -（下述匿名管道 HTTP 验证） | 0 | 入口 200，资产 200，未认证入口 401；版本 0.1.0 与 mygo.config.ts 相同；token 不落日志；退出 0。 |
| Diff | git diff --check | 0 | 无空白错误。 |

Headless 验证通过匿名管道传递 URL，使用空临时工作目录，不移动或修改 web/dist。
等价可复跑命令（终端不打印 token）：

```sh
python3 - <<'PY'
from pathlib import Path
import json, os, re, select, signal, subprocess, tempfile, urllib.request, urllib.error
expected = re.search(r"version:\s*'([^']+)'", Path('mygo.config.ts').read_text())[1]
reader, writer = os.pipe()
with tempfile.TemporaryDirectory() as cwd, tempfile.TemporaryFile(mode='w+') as logs:
    p = subprocess.Popen(['/tmp/inferyard-studio-t8-darwin', '-headless', '-session-fd', str(writer)],
                         cwd=cwd, stdout=logs, stderr=logs, pass_fds=(writer,))
    os.close(writer)
    try:
        assert select.select([reader], [], [], 10)[0]
        url = os.read(reader, 4096).decode().strip()
        base, token = url.split('/?')[0], url.split('token=')[1]
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        with opener.open(url, timeout=5) as response:
            assert response.status == 200
            html = response.read().decode()
        req = urllib.request.Request(base + '/api/status', headers={'Authorization': 'Bearer ' + token})
        with opener.open(req, timeout=5) as response:
            assert json.load(response)['version'] == expected
        for asset in re.findall(r'(?:src|href)="(/assets/[^\"]+)"', html):
            with opener.open(base + asset, timeout=5) as response:
                assert response.status == 200
                print('asset bytes:', len(response.read()))
        try:
            opener.open(base + '/', timeout=5)
            raise AssertionError('unauthenticated entry accepted')
        except urllib.error.HTTPError as error:
            assert error.code == 401
        p.send_signal(signal.SIGINT)
        assert p.wait(timeout=5) == 0
        logs.seek(0)
        assert token not in logs.read()
        print('version/auth/assets/private session/exit: PASS')
    finally:
        os.close(reader)
        if p.poll() is None:
            p.kill()
            p.wait()
PY
```

## 未验证项与边界

- 未启动真实模型、原生桌面窗口，未在 Windows/Linux 实机运行。
- 本轮没有运行会移动 web/dist 的 verify-embed.py，以免干扰并行前端 worker；已做空工作目录下的嵌入资产 HTTP 实测。
- S1 使用相邻 500ms 窗口近似每秒采样周期；未以真实长时间硬件采样验证时间跨度超过窗口的采集行为。
- 根目录访问/遍历失败和已知 host marker 格式错误仍返回服务错误；per-run 降级不掩盖这些全局问题。
- Nowledge 精确线程搜索 `InferYard Studio T8` 返回 0 条；本轮以指定审查报告和现有源码为依据。
