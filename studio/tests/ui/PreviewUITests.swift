import AppKit
import XCTest

@MainActor
final class PreviewUITests: XCTestCase {
    var app: XCUIApplication!
    var currentScene = "任务 · 准备"

    override func setUpWithError() throws {
        continueAfterFailure = false
        app = XCUIApplication()
        app.launchArguments = ["--debug-ui"]
        app.launch()
        expectExists(app.windows["InferYard Studio · UI 调试"], timeout: 10)
        positionPreviewWindowForCapture(app.windows["InferYard Studio · UI 调试"])
        expectText("全部为模拟数据；操作仅改变本窗口，不运行模型或写入文件。")
    }

    override func tearDownWithError() throws {
        if let app, app.state != .notRunning {
            if let testRun, testRun.totalFailureCount > 0 {
                if app.windows["InferYard Studio · UI 调试"].exists {
                    keepScreenshot("Failed preview window")
                }
                let tree = XCTAttachment(string: app.debugDescription)
                tree.name = "Accessibility tree"
                tree.lifetime = .keepAlways
                add(tree)
            }
            app.terminate()
        }
    }

    func testPreviewWorkflowsInOneWindow() {
        let host = NSRunningApplication.runningApplications(withBundleIdentifier: "local.inferyard.preview-e2e")
        XCTAssertEqual(host.count, 1)
        let process = host.first?.processIdentifier
        XCTAssertEqual(app.state, .runningForeground)
        XCTContext.runActivity(named: "BenchmarkFromPreparationToReport") { _ in
            benchmarkFromPreparationToReport()
        }
        XCTContext.runActivity(named: "FailureCanRetryThroughNormalControls") { _ in
            selectScene("任务 · 待运行")
            failureCanRetryThroughNormalControls()
        }
        XCTContext.runActivity(named: "CancellationAllowsAnotherTask") { _ in
            selectScene("任务 · 待运行")
            cancellationAllowsAnotherTask()
        }
        XCTContext.runActivity(named: "DownloadFailureRetryAndReset") { _ in
            selectScene("任务 · 准备")
            downloadFailureRetryAndReset()
        }
        XCTContext.runActivity(named: "ContextSpeedResultAndHistory") { _ in
            selectScene("任务 · 待运行")
            contextSpeedResultAndHistory()
        }
        XCTContext.runActivity(named: "ContextSpeedCancellationDiagnostics") { _ in
            selectScene("任务 · 待运行")
            contextSpeedCancellationDiagnostics()
        }
        XCTContext.runActivity(named: "ContextSpeedFailureMissingAndReadRetry") { _ in
            selectScene("任务 · 待运行")
            contextSpeedFailureMissingAndReadRetry()
        }
        XCTContext.runActivity(named: "FirstUseWithoutLocalAssets") { _ in
            selectScene("首次使用 · 无资源")
            firstUseWithoutLocalAssets()
        }
        XCTContext.runActivity(named: "ResourceChartsAndSourceSelection") { _ in
            selectScene("资源 · 密集采样与缺测")
            resourceChartsAndSourceSelection()
        }
        XCTContext.runActivity(named: "EngineStatusPanel") { _ in
            selectScene("任务 · 失败")
            engineStatusPanel()
        }
        XCTContext.runActivity(named: "LocalDirectoryEngineRunsBothScopes") { _ in
            selectScene("引擎 · 本地安装与目录模型")
            localDirectoryEngineRunsBothScopes()
        }
        XCTContext.runActivity(named: "LMStudioBindingFailureKeepsCompletedScope") { _ in
            selectScene("引擎 · LM Studio 手动绑定")
            lmStudioBindingFailureKeepsCompletedScope()
        }
        XCTAssertEqual(app.windows.count, 1)
        let remaining = NSRunningApplication.runningApplications(withBundleIdentifier: "local.inferyard.preview-e2e")
        XCTAssertEqual(remaining.count, 1)
        XCTAssertEqual(remaining.first?.processIdentifier, process, "All workflows must keep the same app process")
    }

    private func benchmarkFromPreparationToReport() {
        prepare()
        XCTAssertFalse(text("Dashboard").exists)
        keepScreenshot("Benchmark setup with single stage heading")
        click("下载")
        click("下载模型")
        expectText("已结束")
        click("模型")
        let picker = app.popUpButtons["模型"]
        expectExists(picker, timeout: 5)
        picker.click()
        let downloadedModel = "model · SmolLM2-135M-Instruct-Q4_K_M.gguf"
        text(downloadedModel).click()
        click("用这个组合新开任务")
        click("运行 benchmark")
        expectExists(app.buttons["查看实时详情"], timeout: 10)
        XCTAssertFalse(text("Dashboard").exists)
        keepScreenshot("Task running without obsolete title")
        click("查看实时详情", timeout: 10)
        expectText("实时进度")
        let progress = app.staticTexts.matching(NSPredicate(format: "value BEGINSWITH %@", "正式运行 · ")).firstMatch
        expectExists(progress, timeout: 5)
        let before = progress.value as? String
        wait("detail progress changes", timeout: 10) { (progress.value as? String) != before }
        click("返回运行")
        click("查看报告", timeout: 25)
        expectText(downloadedModel)
        expectText("120 个有效样本")
        expectText("各类任务表现")
        XCTAssertTrue(app.popUpButtons["选择运行"].exists)
        click("新任务")
        click("报告")
        expectText("历史任务")
        let report = app.tables["报告列表"].staticTexts.matching(NSPredicate(format: "value BEGINSWITH %@", "preview-report-")).firstMatch
        XCTAssertTrue(report.exists, "New task must preserve the generated report")
        XCTAssertEqual(app.tables["报告列表"].tableRows.count, 2, "One report plus the header")
    }

    private func failureCanRetryThroughNormalControls() {
        app.checkBoxes["下次操作模拟失败"].click()
        click("运行 benchmark")
        click("启动服务", timeout: 10)
        click("查看报告", timeout: 25)
        expectText("Preview-Model-Q4.gguf")
        expectText("120 个有效样本")
    }

    private func cancellationAllowsAnotherTask() {
        click("运行 benchmark")
        click("查看实时详情", timeout: 10)
        click("取消任务")
        click("运行 benchmark", timeout: 10)
        click("查看报告", timeout: 25)
        expectText("各类任务表现")
    }

    private func downloadFailureRetryAndReset() {
        click("下载")
        app.checkBoxes["下次操作模拟失败"].click()
        click("下载模型")
        expectText("下载失败，请检查网络后重试。 · download_failed")
        click("重新创建下载任务")
        expectText("已结束")
        expectText("实际目录名：model-2")
        expectText("下载失败，请检查网络后重试。 · download_failed")
        click("新任务")
        click("下载")
        expectText("实际目录名：model-2")
        click("重置场景")
        click("下载")
        XCTAssertFalse(app.staticTexts["实际目录名：model-2"].exists)
        XCTAssertFalse(text("下载失败，请检查网络后重试。 · download_failed").exists)
        click("下载模型")
        expectText("已结束")
        expectText("实际目录名：model")
    }

    private func contextSpeedResultAndHistory() {
        prepareSpeed()
        openMenu("上下文档位")
        app.menuItems["128k"].click()
        keepScreenshot("Context speed setup")
        click("开始专项测评")
        expectText("已完成 1 / 5 次测量")
        keepScreenshot("Context speed progress")
        click("查看结果", timeout: 15)
        expectText("有效 5/5 · 中位数")
        expectText("106294")
        for column in ["模型 / 引擎", "Context", "Prompt length (tokens)", "Time (s)", "Prefill (tok/s)", "Decode (tok/s)"] {
            expectText(column)
            let header = text(column)
            XCTAssertTrue(header.isHittable)
            XCTAssertLessThanOrEqual(header.frame.maxX, app.windows["InferYard Studio · UI 调试"].frame.maxX - 16)
        }
        XCTAssertFalse(text("各类任务表现").exists)
        XCTAssertTrue(text("128k").exists)
        XCTAssertFalse(text("131072").exists)
        keepScreenshot("Context speed result")
        app.disclosureTriangles["查看逐次测量"].click()
        let measurements = app.tables["专项逐次测量"]
        expectExists(measurements, timeout: 5)
        XCTAssertEqual(measurements.tableRows.count, 6)
        click("返回历史任务")
        let row = app.tables["专项历史表"].tableRows.element(boundBy: 1)
        expectExists(row, timeout: 5)
        row.click()
        click("打开专项结果")
        expectText("有效 5/5 · 中位数")
        click("用此组合新开任务")
        expectText("先停止当前受管服务，再按所选上下文启动新任务。")
        click("停止当前服务")
        click("开始专项测评")
        expectText("已完成 1 / 5 次测量")
    }

    private func contextSpeedCancellationDiagnostics() {
        prepareSpeed()
        click("开始专项测评")
        click("取消专项测评")
        click("查看结果", timeout: 10)
        expectText("有效 0/5 · 未形成完整汇总")
        expectText("任务已取消。已完成的测量已保留；未完成项显示为 —。")
        XCTAssertFalse(text("第 1 次缺测：Context: not_executed；Prompt length: not_executed；Time: not_executed；Prefill: not_executed；Decode: not_executed").exists)
        keepScreenshot("Context speed cancelled")
        let diagnostics = app.disclosureTriangles["查看诊断详情"]
        revealInContent(diagnostics)
        diagnostics.click()
        expectText("第 1 次缺测：Context: not_executed；Prompt length: not_executed；Time: not_executed；Prefill: not_executed；Decode: not_executed")
        keepScreenshot("Context speed cancellation diagnostics")
    }

    private func contextSpeedFailureMissingAndReadRetry() {
        prepareSpeed()
        app.checkBoxes["下次操作模拟失败"].click()
        click("开始专项测评")
        expectText("engine_start_failed")
        expectText("已完成 0 / 5 次测量")
        click("用此组合新开任务")
        app.checkBoxes["下次专项模拟缺测"].click()
        click("开始专项测评")
        click("查看结果", timeout: 15)
        expectText("有效 4/5 · 未形成完整汇总")
        expectText("测量结果不满足完整汇总条件；请展开诊断详情查看缺测或校验原因。")
        XCTAssertFalse(text("第 5 次：final_timing_missing、metric_missing").exists)
        keepScreenshot("Context speed missing timing")
        let diagnostics = app.disclosureTriangles["查看诊断详情"]
        revealInContent(diagnostics)
        diagnostics.click()
        expectText("第 5 次：final_timing_missing、metric_missing")
        expectText("第 5 次缺测：Time: final_timing_missing；Prefill: final_timing_missing；Decode: final_timing_missing")
        keepScreenshot("Context speed missing timing diagnostics")
        click("返回历史任务")
        let row = app.tables["专项历史表"].tableRows.element(boundBy: 2)
        expectExists(row, timeout: 5)
        row.click()
        app.checkBoxes["下次操作模拟失败"].click()
        click("打开专项结果")
        expectText("模拟结果读取失败；重新读取不会再次运行模型。")
        keepScreenshot("Context speed read error")
        click("重新读取结果")
        expectText("有效 4/5 · 未形成完整汇总")
    }

    private func prepareSpeed() {
        openMenu("测评类型")
        app.menuItems["长上下文速度"].click()
        expectText("zh-context-speed · 5 次测量 · 每次输出 256 tokens")
    }

    private func firstUseWithoutLocalAssets() {
        click("准备环境")
        expectText("准备模型与引擎")
        XCTAssertFalse(text("工作区进度").exists)
        XCTAssertFalse(app.staticTexts.matching(NSPredicate(format: "value CONTAINS %@", "尚未识别的原因码")).firstMatch.exists)
        keepScreenshot("First use with no local assets")
        click("到下载页")
        #if arch(arm64)
        expectText("推荐资源 · macOS arm64")
        #else
        expectText("推荐资源 · macOS x64")
        #endif
        XCTAssertFalse(text("Windows x64").exists)
        XCTAssertFalse(text("Linux x64").exists)
        keepScreenshot("Recommendations for this Mac")
        let qwen = "Qwen3-0.6B-Q8_0（GGUF）"
        let smol = "SmolLM2-135M-Instruct-Q4_K_M（GGUF）"
        click("选择 · " + qwen)
        expectExists(app.buttons["已选择 · " + qwen], timeout: 3)
        XCTAssertFalse(app.textFields["模型 URL（固定 40 位 revision 的 GGUF）"].exists)
        keepScreenshot("Model recommendations and selected Qwen")
        click("选择 · " + smol)
        click("下载引擎")
        click("下载模型")
        click("返回任务")
        wait("downloaded resources unlock benchmark", timeout: 15) {
            self.app.buttons["运行 benchmark"].exists && self.app.buttons["运行 benchmark"].isEnabled
        }
        XCTAssertEqual(String(describing: app.checkBoxes["zh-core"].value ?? ""), "1")
        XCTAssertEqual(String(describing: app.checkBoxes["zh-svg-pelican"].value ?? ""), "1")
        revealInContent(app.buttons["运行 benchmark"])
        keepScreenshot("First use ready to benchmark")
        click("运行 benchmark")
        expectText("zh-core · 运行中")
        expectText("zh-svg-pelican · 排队中")
        XCTAssertFalse(text("单次运行不支持断点续跑；请重新运行该配置。").exists)
        keepScreenshot("Active benchmark without recovery warning")
        let engine = app.buttons["引擎 · 运行中"]
        expectExists(engine, timeout: 10)
        engine.hover()
        let stop = app.buttons["停止引擎"]
        expectExists(stop, timeout: 3)
        XCTAssertFalse(stop.isEnabled, "Running tasks must protect the engine")
        keepScreenshot("Engine panel remains available during benchmark")
        let queue = app.buttons.matching(NSPredicate(format: "title BEGINSWITH %@", "任务 · 执行 1 · 等待 ")).firstMatch
        XCTAssertTrue(queue.exists)
        queue.click()
        expectText("任务队列", timeout: 3)
        keepScreenshot("Task icon opens live queue panel")
        queue.click()
        click("查看报告", timeout: 25)
        expectText("各类任务表现")
    }

    private func resourceChartsAndSourceSelection() {
        expectText("采样记录：316 · 每个来源保留最近 600 点")
        expectText("服务 CPU % · 此运行未记录该指标")
        expectText("传感器温度 °C · 此运行未记录该指标")
        let memoryChart = text("可用内存 GiB曲线")
        revealInContent(memoryChart)
        XCTAssertTrue(app.windows["InferYard Studio · UI 调试"].frame.contains(memoryChart.frame), "The full resource chart must be visible")
        keepScreenshot("Dense memory samples without empty CPU and temperature plots")
        selectScene("资源 · CPU 与多路温度")
        expectText("采样记录：160 · 每个来源保留最近 600 点")
        expectText("服务 CPU %曲线")
        expectText("传感器温度 °C曲线")
        let source = app.popUpButtons["传感器温度 °C来源"]
        revealInContent(source)
        XCTAssertTrue(source.isHittable)
        keepScreenshot("CPU curve and first temperature sensor")
        source.click()
        text("AppleSMC:Tp0C").click()
        expectText("AppleSMC:Tp0C")
        let chart = text("传感器温度 °C曲线")
        revealInContent(chart)
        XCTAssertTrue(chart.isHittable)
        XCTAssertTrue(app.tables["资源采样数值表"].staticTexts.matching(NSPredicate(format: "value == %@", "AppleSMC:Tp0C")).firstMatch.exists)
        keepScreenshot("Selected temperature source with its own measurements")
    }

    private func engineStatusPanel() {
        let engine = app.buttons["引擎 · 失败"]
        expectExists(engine, timeout: 5)
        engine.hover()
        expectExists(app.buttons["开启引擎"], timeout: 5)
        keepScreenshot("Engine hover panel before startup")
        app.windows["InferYard Studio · UI 调试"].hover()
        wait("hover panel closes after pointer leaves", timeout: 5) { !self.app.buttons["开启引擎"].exists }
        engine.click()
        expectExists(app.buttons["开启引擎"], timeout: 5)
        keepScreenshot("Engine panel opened by clicking status icon")
        click("开启引擎")
        let running = app.buttons["引擎 · 运行中"]
        expectExists(running, timeout: 5)
        running.hover()
        expectExists(app.buttons["停止引擎"], timeout: 5)
        XCTAssertFalse(app.buttons["查看实时详情"].exists, "Engine startup must not start a benchmark")
        keepScreenshot("Engine running icon and hover controls")
        click("停止引擎")
        expectExists(app.buttons["引擎 · 已停止"], timeout: 5)
    }

    private func localDirectoryEngineRunsBothScopes() {
        expectExists(app.menuButtons["local-weights · 目录模型"], timeout: 5)
        revealInContent(app.buttons["运行 benchmark"])
        keepScreenshot("Local directory model and registered engine")
        click("运行 benchmark")
        expectText("zh-core · 运行中")
        expectText("zh-svg-pelican · 排队中")
        keepScreenshot("Local first scope and queued second scope")
        expectText("zh-svg-pelican · 运行中", timeout: 15)
        keepScreenshot("Local second scope after service handoff")
        click("查看报告", timeout: 20)
        expectText("local-weights")
        expectText("各类任务表现")
    }

    private func lmStudioBindingFailureKeepsCompletedScope() {
        revealInContent(app.buttons["运行 benchmark"])
        click("运行 benchmark")
        expectText("zh-svg-pelican · 等待绑定 LM Studio", timeout: 15)
        let pid = app.textFields["新的后台 PID"]
        expectExists(pid, timeout: 5)
        revealInContent(pid)
        keepScreenshot("LM Studio waits for a fresh second-scope service")
        pid.click()
        pid.typeText("4002")
        app.checkBoxes["下次操作模拟失败"].click()
        click("绑定并继续此题包")
        click("重试未完成的题包", timeout: 10)
        expectExists(pid, timeout: 5)
        revealInContent(pid)
        pid.click()
        pid.typeText("4003")
        click("绑定并继续此题包")
        expectText("zh-svg-pelican · 运行中")
        XCTAssertFalse(text("zh-core · 运行中").exists, "A completed scope must not run again")
        keepScreenshot("LM Studio retries only the unfinished scope")
        click("查看报告", timeout: 20)
        expectText("local.gguf")
        expectText("各类任务表现")
    }

}
