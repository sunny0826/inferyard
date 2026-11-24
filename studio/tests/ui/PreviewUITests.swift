import XCTest

@MainActor
final class PreviewUITests: XCTestCase {
    var app: XCUIApplication!
    var currentScene = "任务 · 准备"
    private var suite = "full"

    override func setUpWithError() throws {
        continueAfterFailure = false
        suite = ProcessInfo.processInfo.environment["INFERYARD_UI_SUITE"] ?? "full"
        guard ["daily", "full"].contains(suite) else {
            XCTFail("Unknown native UI suite: \(suite)")
            return
        }
        app = XCUIApplication()
        app.launchArguments = ["--debug-ui"]
        app.launch()
        expectExists(app.windows["InferYard Studio · UI 调试"], timeout: 10)
        positionPreviewWindowForCapture(app.windows["InferYard Studio · UI 调试"])
        expectText("模拟数据模式")
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
        let workflows: [(name: String, scene: String?, daily: Bool, run: () -> Void)] = [
            ("BenchmarkFromPreparationToReport", nil, false, benchmarkFromPreparationToReport),
            ("CancellationAllowsAnotherTask", "任务 · 待运行", false, cancellationAllowsAnotherTask),
            ("ContextSpeedResultAndHistory", "任务 · 待运行", true, contextSpeedResultAndHistory),
            ("ContextSpeedFailureMissingAndReadRetry", "任务 · 待运行", false, contextSpeedFailureMissingAndReadRetry),
            ("FirstUseWithoutLocalAssets", "首次使用 · 无资源", true, firstUseWithoutLocalAssets),
            ("ResourceChartsAndSourceSelection", "资源 · 密集采样与缺测", true, resourceChartsAndSourceSelection),
            ("LMStudioBindingFailureKeepsCompletedScope", "引擎 · LM Studio 手动绑定", false, lmStudioBindingFailureKeepsCompletedScope),
        ]
        let selected = workflows.filter { suite == "full" || $0.daily }
        let coverage = XCTAttachment(string: "Suite: \(suite)\n" + selected.map(\.name).joined(separator: "\n"))
        coverage.name = "Native UI suite coverage"
        coverage.lifetime = .keepAlways
        add(coverage)
        for workflow in selected {
            XCTContext.runActivity(named: workflow.name) { _ in
                if let scene = workflow.scene { selectScene(scene) }
                workflow.run()
            }
        }
    }

    private func benchmarkFromPreparationToReport() {
        prepare()
        expectText("架构 llama · 约 4.0 GiB")
        expectBundleLabels()
        keepScreenshot("Benchmark setup with single stage heading")
        click("下载")
        selectTab("模型")
        click("下载模型 · model-qwen3-0.6b-q8")
        expectText("已结束")
        click("模型")
        let downloadedModel = "model · Qwen3-0.6B-Q8_0.gguf"
        let downloadedModelLabel = downloadedModel + " · GGUF"
        expectText(downloadedModelLabel)
        XCTAssertFalse(app.popUpButtons["模型"].exists, "Models must not offer an inline pair selector")
        // The sidebar task entry switches to the current task without resetting
        // or confirming; the pair is chosen on the task page.
        click("任务")
        chooseModel(downloadedModelLabel)
        XCTAssertFalse(app.buttons["确认开始新任务"].exists, "Task nav must not ask to confirm")
        click("开始测评")
        expectExists(app.buttons["查看实时详情"], timeout: 10)
        keepScreenshot("Task running without obsolete title")
        click("查看实时详情", timeout: 10)
        expectText("实时进度")
        let progress = app.staticTexts.matching(NSPredicate(format: "value BEGINSWITH %@", "正式运行 · ")).firstMatch
        expectExists(progress, timeout: 5)
        let before = progress.value as? String
        wait("detail progress changes", timeout: 10) { (progress.value as? String) != before }
        click("← 返回任务")
        click("查看报告", timeout: 25)
        expectText(downloadedModel)
        expectText("120 个有效样本")
        expectText("各类任务表现")
        expectText("吞吐")
        expectText("输出内容块到达间隔")
        revealInContent(text("输出内容块到达间隔"))
        revealInContent(text("间隔详情"))
        keepScreenshot("Report throughput and output-interval cards")
        click("新任务")
        click("报告")
        expectTab("测评报告")
        selectTab("运行历史")
        expectText("运行历史表")
        selectTab("测评报告")
        // MyGo exports a button's name as its accessibility title, never its label.
        let report = app.buttons.matching(NSPredicate(format: "title BEGINSWITH %@", "打开报告 · preview-report-")).firstMatch
        wait("New task must preserve the generated report", timeout: 5) { report.exists }
    }

    private func cancellationAllowsAnotherTask() {
        click("开始测评")
        click("查看实时详情", timeout: 10)
        click("取消任务")
        expectExists(app.buttons["确认取消任务"], timeout: 5)
        keepScreenshot("Cancel asks before discarding the run")
        click("确认取消任务")
        click("开始测评", timeout: 10)
        click("查看报告", timeout: 25)
        expectText("各类任务表现")
    }

    private func contextSpeedResultAndHistory() {
        prepareSpeed()
        let context128 = app.radioButtons["128k"]
        expectExists(context128, timeout: 3)
        context128.click()
        keepScreenshot("Context speed setup")
        click("开始专项测评")
        expectText("已完成 1 / 5 次测量")
        keepScreenshot("Context speed progress")
        click("查看结果", timeout: 15)
        expectText("有效 5/5 · 中位数")
        expectText("106294")
        for column in ["模型 / 引擎", "上下文（tokens）", "输入长度（tokens）", "输入处理（s）", "Prefill（tok/s）", "Decode（tok/s）"] {
            expectText(column)
        }
        XCTAssertFalse(text("各类任务表现").exists)
        XCTAssertTrue(text("128k").exists)
        XCTAssertFalse(text("131072").exists)
        keepScreenshot("Context speed result")
        expandFold("查看逐次测量")
        let measurements = app.tables["专项逐次测量"]
        expectExists(measurements, timeout: 5)
        click("返回历史任务")
        let row = app.tables["专项历史表"].tableRows.element(boundBy: 1)
        expectExists(row, timeout: 5)
        row.click()
        click("打开专项结果")
        expectText("有效 5/5 · 中位数")
        click("用此组合新开任务")
        expectText("先停止当前受管服务，再按所选上下文启动新任务。")
        click("停止当前服务")
        expectExists(app.buttons["确认停止当前服务"], timeout: 5)
        click("确认停止当前服务")
        click("开始专项测评")
        expectText("已完成 1 / 5 次测量")
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
        expectText("未形成完整汇总，原因见下方诊断详情。")
        XCTAssertFalse(text("第 5 次：final_timing_missing、metric_missing").exists)
        keepScreenshot("Context speed missing timing")
        let diagnostics = app.disclosureTriangles["查看诊断详情"]
        revealInContent(diagnostics)
        diagnostics.click()
        expectText("第 5 次：final_timing_missing、metric_missing")
        expectText("第 5 次缺测：输入处理: final_timing_missing；Prefill: final_timing_missing；Decode: final_timing_missing")
        keepScreenshot("Context speed missing timing diagnostics")
        click("返回历史任务")
        let row = app.tables["专项历史表"].tableRows.element(boundBy: 2)
        expectExists(row, timeout: 5)
        row.click()
        app.checkBoxes["下次操作模拟失败"].click()
        click("打开专项结果")
        expectText("模拟结果读取失败，可重试。")
        keepScreenshot("Context speed read error")
        click("重新读取结果")
        expectText("有效 4/5 · 未形成完整汇总")
    }

    private func firstUseWithoutLocalAssets() {
        expectFirstUseGuide()
        click("准备环境")
        expectText("准备模型与引擎")
        XCTAssertFalse(text("工作区进度").exists)
        XCTAssertFalse(app.staticTexts.matching(NSPredicate(format: "value CONTAINS %@", "操作失败（")).firstMatch.exists)
        keepScreenshot("First use with no local assets")
        click("到下载页")
        expectText("推荐组合")
        expectText("PolyStrata")
        XCTAssertFalse(text("Windows x64").exists)
        XCTAssertFalse(text("Linux x64").exists)
        keepScreenshot("Recommendations for this Mac")
        click("一键安装 PolyStrata")
        click("返回任务")
        wait("downloaded resources unlock benchmark", timeout: 15) {
            self.app.buttons["开始测评"].exists && self.app.buttons["开始测评"].isEnabled
        }
        XCTAssertEqual(String(describing: app.checkBoxes["zh-core"].value ?? ""), "1")
        XCTAssertEqual(String(describing: app.checkBoxes["zh-svg-pelican"].value ?? ""), "1")
        expectBundleLabels()
        revealInContent(app.buttons["开始测评"])
        keepScreenshot("First use ready to benchmark")
        click("开始测评")
        expectText("zh-core · 运行中")
        expectText("zh-svg-pelican · 排队中")
        XCTAssertFalse(text("单次运行不支持断点续跑；请重新运行该配置。").exists)
        keepScreenshot("Active benchmark without recovery warning")
        let engine = app.buttons["引擎 · 运行中"]
        expectExists(engine, timeout: 10)
        engine.click()
        let stop = app.buttons["停止引擎"]
        expectExists(stop, timeout: 3)
        XCTAssertFalse(stop.isEnabled, "Running tasks must protect the engine")
        keepScreenshot("Engine panel remains available during benchmark")
        engine.click()
        wait("engine panel closed after a second click", timeout: 5) { !self.app.buttons["停止引擎"].exists }
        let queue = app.buttons.matching(NSPredicate(format: "title BEGINSWITH %@", "任务 · 执行 1 · 等待 ")).firstMatch
        expectExists(queue, timeout: 5)
        queue.click()
        expectText("任务队列", timeout: 3)
        keepScreenshot("Task icon opens live queue panel")
        queue.click()
        click("查看报告", timeout: 25)
        expectText("各类任务表现")
    }

    private func resourceChartsAndSourceSelection() {
        expectText("服务 CPU % · —")
        expectText("传感器温度 °C · —")
        let memoryChart = text("可用内存 GiB曲线")
        revealInContent(memoryChart)
        keepScreenshot("Dense memory samples without empty CPU and temperature plots")
        selectScene("资源 · CPU 与多路温度")
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
        // The sample table lives inside a fold; expand it before asserting rows.
        expandFold("原始采样数值")
        let table = app.tables["资源采样数值表"]
        expectExists(table, timeout: 5)
        XCTAssertTrue(table.staticTexts.matching(NSPredicate(format: "value == %@", "AppleSMC:Tp0C")).firstMatch.exists)
        keepScreenshot("Selected temperature source with its own measurements")
    }

    private func lmStudioBindingFailureKeepsCompletedScope() {
        revealInContent(app.buttons["开始测评"])
        click("开始测评")
        expectText("zh-svg-pelican · 等待连接 LM Studio", timeout: 15)
        let pid = app.textFields["新的后台 PID"]
        expectExists(pid, timeout: 5)
        revealInContent(pid)
        keepScreenshot("LM Studio waits for a fresh second-scope service")
        pid.click()
        pid.typeText("4002")
        app.checkBoxes["下次操作模拟失败"].click()
        click("连接并继续此测评题集")
        click("重试未完成的测评题集", timeout: 10)
        expectExists(pid, timeout: 5)
        revealInContent(pid)
        pid.click()
        pid.typeText("4003")
        click("连接并继续此测评题集")
        expectText("zh-svg-pelican · 运行中")
        XCTAssertFalse(text("zh-core · 运行中").exists, "A completed scope must not run again")
        keepScreenshot("LM Studio retries only the unfinished scope")
        click("查看报告", timeout: 20)
        expectText("local.gguf")
        expectText("各类任务表现")
    }

}
