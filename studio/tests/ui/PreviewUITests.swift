import AppKit
import XCTest

@MainActor
final class PreviewUITests: XCTestCase {
    private var app: XCUIApplication!

    override func setUpWithError() throws {
        continueAfterFailure = false
        app = XCUIApplication()
        app.launchArguments = ["--debug-ui"]
        app.launch()
        XCTAssertTrue(app.windows["InferYard Studio · UI 调试"].waitForExistence(timeout: 10))
        positionWindowForCapture()
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

    func testBenchmarkFromPreparationToReport() {
        prepare()
        XCTAssertFalse(text("Dashboard").exists)
        keepScreenshot("Benchmark setup with single stage heading")
        click("下载")
        click("下载模型")
        expectText("已结束")
        click("模型")
        let picker = app.popUpButtons["模型"]
        XCTAssertTrue(picker.waitForExistence(timeout: 5))
        picker.click()
        let downloadedModel = "model · SmolLM2-135M-Instruct-Q4_K_M.gguf"
        text(downloadedModel).click()
        click("用这个组合新开任务")
        click("运行 benchmark")
        XCTAssertTrue(app.buttons["查看实时详情"].waitForExistence(timeout: 10))
        XCTAssertFalse(text("Dashboard").exists)
        keepScreenshot("Task running without obsolete title")
        click("查看实时详情", timeout: 10)
        expectText("实时进度")
        let progress = app.staticTexts.matching(NSPredicate(format: "value BEGINSWITH %@", "正式运行 · ")).firstMatch
        XCTAssertTrue(progress.waitForExistence(timeout: 5))
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

    func testFailureCanRetryThroughNormalControls() {
        prepare()
        app.checkBoxes["下次操作模拟失败"].click()
        click("运行 benchmark")
        click("启动服务", timeout: 10)
        click("查看报告", timeout: 25)
        expectText("Preview-Model-Q4.gguf")
        expectText("120 个有效样本")
    }

    func testCancellationAllowsAnotherTask() {
        prepare()
        click("运行 benchmark")
        click("查看实时详情", timeout: 10)
        click("取消任务")
        click("运行 benchmark", timeout: 10)
        click("查看报告", timeout: 25)
        expectText("各类任务表现")
    }

    func testDownloadFailureRetryAndReset() {
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

    func testContextSpeedResultAndHistory() {
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
        XCTAssertTrue(measurements.waitForExistence(timeout: 5))
        XCTAssertEqual(measurements.tableRows.count, 6)
        click("返回历史任务")
        let row = app.tables["专项历史表"].tableRows.element(boundBy: 1)
        XCTAssertTrue(row.waitForExistence(timeout: 5))
        row.click()
        click("打开专项结果")
        expectText("有效 5/5 · 中位数")
        click("用此组合新开任务")
        expectText("先停止当前受管服务，再按所选上下文启动新任务。")
        click("停止当前服务")
        click("开始专项测评")
        click("取消专项测评")
        click("查看结果", timeout: 10)
        expectText("有效 0/5 · 未形成完整汇总")
        expectText("任务已取消。已完成的测量已保留；未完成项显示为 —。")
        XCTAssertFalse(text("第 1 次缺测：Context: not_executed；Prompt length: not_executed；Time: not_executed；Prefill: not_executed；Decode: not_executed").exists)
        keepScreenshot("Context speed cancelled")
        app.disclosureTriangles["查看诊断详情"].click()
        expectText("第 1 次缺测：Context: not_executed；Prompt length: not_executed；Time: not_executed；Prefill: not_executed；Decode: not_executed")
        keepScreenshot("Context speed cancellation diagnostics")
    }

    func testContextSpeedFailureMissingAndReadRetry() {
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
        app.disclosureTriangles["查看诊断详情"].click()
        expectText("第 5 次：final_timing_missing、metric_missing")
        expectText("第 5 次缺测：Time: final_timing_missing；Prefill: final_timing_missing；Decode: final_timing_missing")
        keepScreenshot("Context speed missing timing diagnostics")
        click("返回历史任务")
        let row = app.tables["专项历史表"].tableRows.element(boundBy: 2)
        XCTAssertTrue(row.waitForExistence(timeout: 5))
        row.click()
        app.checkBoxes["下次操作模拟失败"].click()
        click("打开专项结果")
        expectText("模拟结果读取失败；重新读取不会再次运行模型。")
        keepScreenshot("Context speed read error")
        click("重新读取结果")
        expectText("有效 4/5 · 未形成完整汇总")
    }

    private func prepareSpeed() {
        prepare()
        openMenu("测评类型")
        app.menuItems["长上下文速度"].click()
        expectText("zh-context-speed · 5 次测量 · 每次输出 256 tokens")
    }

    func testFirstUseWithoutLocalAssets() {
        app.popUpButtons["模拟场景"].click()
        text("首次使用 · 无资源").click()
        click("准备环境")
        expectText("准备模型与引擎")
        XCTAssertFalse(text("工作区进度").exists)
        XCTAssertFalse(app.staticTexts.matching(NSPredicate(format: "value CONTAINS %@", "尚未识别的原因码")).firstMatch.exists)
        keepScreenshot("First use with no local assets")
        click("到下载页")
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
        click("查看报告", timeout: 25)
        expectText("各类任务表现")
    }

    func testResourceChartWithMissingMetrics() {
        app.popUpButtons["模拟场景"].click()
        text("资源 · 密集采样与缺测").click()
        expectText("采样记录：316 · 每个来源保留最近 600 点")
        expectText("服务 CPU % · 此运行未记录该指标")
        expectText("传感器温度 °C · 此运行未记录该指标")
        let chart = text("可用内存 GiB曲线")
        revealInContent(chart)
        XCTAssertTrue(app.windows["InferYard Studio · UI 调试"].frame.contains(chart.frame), "The full resource chart must be visible")
        keepScreenshot("Dense memory samples without empty CPU and temperature plots")
    }

    func testCPUAndTemperatureSources() {
        app.popUpButtons["模拟场景"].click()
        text("资源 · CPU 与多路温度").click()
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

    func testEngineStatusPanel() {
        app.popUpButtons["模拟场景"].click()
        text("任务 · 失败").click()
        let engine = app.buttons["引擎 · 失败"]
        XCTAssertTrue(engine.waitForExistence(timeout: 5))
        engine.hover()
        XCTAssertTrue(app.buttons["开启引擎"].waitForExistence(timeout: 5))
        keepScreenshot("Engine hover panel before startup")
        app.windows["InferYard Studio · UI 调试"].hover()
        wait("hover panel closes after pointer leaves", timeout: 5) { !self.app.buttons["开启引擎"].exists }
        engine.click()
        XCTAssertTrue(app.buttons["开启引擎"].waitForExistence(timeout: 5))
        keepScreenshot("Engine panel opened by clicking status icon")
        click("开启引擎")
        let running = app.buttons["引擎 · 运行中"]
        XCTAssertTrue(running.waitForExistence(timeout: 5))
        running.hover()
        XCTAssertTrue(app.buttons["停止引擎"].waitForExistence(timeout: 5))
        XCTAssertFalse(app.buttons["查看实时详情"].exists, "Engine startup must not start a benchmark")
        keepScreenshot("Engine running icon and hover controls")
        click("停止引擎")
        XCTAssertTrue(app.buttons["引擎 · 已停止"].waitForExistence(timeout: 5))
    }

    func testRunningStatusPanels() {
        prepare()
        click("运行 benchmark")
        let engine = app.buttons["引擎 · 运行中"]
        XCTAssertTrue(engine.waitForExistence(timeout: 10))
        engine.hover()
        let stop = app.buttons["停止引擎"]
        XCTAssertTrue(stop.waitForExistence(timeout: 3))
        XCTAssertFalse(stop.isEnabled, "Running tasks must protect the engine")
        keepScreenshot("Engine panel remains available during benchmark")
        let queue = app.buttons.matching(NSPredicate(format: "title BEGINSWITH %@", "任务 · 执行 1 · 等待 ")).firstMatch
        XCTAssertTrue(queue.exists)
        queue.click()
        expectText("任务队列", timeout: 3)
        keepScreenshot("Task icon opens live queue panel")
    }

    private func positionWindowForCapture() {
        let window = app.windows["InferYard Studio · UI 调试"]
        let screen = NSScreen.screens[0]
        let visible = screen.visibleFrame
        let desktop = CGRect(x: visible.minX, y: screen.frame.maxY - visible.maxY, width: visible.width, height: visible.height)
        let size = CGSize(width: min(1024, desktop.width), height: min(681, desktop.height))
        let corner = window.coordinate(withNormalizedOffset: CGVector(dx: 1, dy: 1)).withOffset(CGVector(dx: -2, dy: -2))
        if window.frame.width > size.width || window.frame.height > size.height {
            corner.press(forDuration: 0.2, thenDragTo: corner.withOffset(CGVector(dx: size.width - window.frame.width, dy: size.height - window.frame.height)))
        }
        let origin = window.frame.origin
        let titlebar = window.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.02))
        let x = desktop.minX + max(0, (desktop.width - window.frame.width) / 2)
        let y = desktop.minY + max(0, (desktop.height - window.frame.height) / 2)
        titlebar.press(forDuration: 0.2, thenDragTo: titlebar.withOffset(CGVector(dx: x - origin.x, dy: y - origin.y)))
        XCTAssertTrue(desktop.insetBy(dx: -1, dy: -1).contains(window.frame), "The test window must fit above the Dock and inside the display")
    }

    private func revealInContent(_ element: XCUIElement) {
        let content = app.scrollViews.element(boundBy: 1)
        for _ in 0..<5 {
            let viewport = content.frame.insetBy(dx: 0, dy: 32)
            let frame = element.frame
            if viewport.contains(frame) { return }
            let delta = frame.maxY > viewport.maxY ? viewport.maxY - frame.maxY : viewport.minY - frame.minY
            content.scroll(byDeltaX: 0, deltaY: delta)
        }
        XCTAssertTrue(content.frame.contains(element.frame), "The complete control must be visible after scrolling")
    }

    private func keepScreenshot(_ name: String) {
        let attachment = XCTAttachment(screenshot: app.windows["InferYard Studio · UI 调试"].screenshot())
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
    }

    private func prepare() {
        click("准备环境")
        wait("benchmark becomes available", timeout: 10) {
            self.app.buttons["运行 benchmark"].exists && self.app.buttons["运行 benchmark"].isEnabled
        }
    }

    private func openMenu(_ label: String) {
        let button = app.menuButtons[label].firstMatch
        wait("menu button \(label) is enabled", timeout: 5) { button.exists && button.isEnabled }
        button.click()
    }

    private func click(_ label: String, timeout: TimeInterval = 5) {
        let button = app.buttons[label].firstMatch
        wait("button \(label) is enabled", timeout: timeout) { button.exists && button.isEnabled }
        button.click()
    }

    private func expectText(_ text: String, timeout: TimeInterval = 10) {
        let element = self.text(text)
        XCTAssertTrue(element.waitForExistence(timeout: timeout), "Missing text: \(text)")
    }

    private func text(_ value: String) -> XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "label == %@ OR value == %@", value, value)).firstMatch
    }

    private func wait(_ description: String, timeout: TimeInterval, condition: @escaping () -> Bool) {
        let expectation = XCTNSPredicateExpectation(predicate: NSPredicate { _, _ in condition() }, object: nil)
        XCTAssertEqual(XCTWaiter.wait(for: [expectation], timeout: timeout), .completed, description)
    }
}
