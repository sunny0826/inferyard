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
        app.checkBoxes["zh-svg-pelican"].click()
        click("运行 benchmark")
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
        app.checkBoxes["zh-svg-pelican"].click()
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
        app.menuItems["128K · 131,072 tokens"].click()
        keepScreenshot("Context speed setup")
        click("开始专项测评")
        expectText("已完成 1 / 5 次测量")
        keepScreenshot("Context speed progress")
        click("查看结果", timeout: 15)
        expectText("有效 5/5 · 中位数")
        expectText("106294")
        for column in ["模型 / 引擎", "Context", "Prompt length (tokens)", "Time (s)", "Prefill (tok/s)", "Decode (tok/s)"] {
            expectText(column)
        }
        XCTAssertFalse(text("各类任务表现").exists)
        keepScreenshot("Context speed result")
        app.disclosureTriangles["查看逐次测量"].click()
        XCTAssertEqual(app.tables["专项逐次测量"].tableRows.count, 6)
        click("返回历史任务")
        let row = app.tables["专项历史表"].tableRows.element(boundBy: 1)
        XCTAssertTrue(row.exists)
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
        keepScreenshot("Context speed cancelled")
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
        expectText("第 5 次：final_timing_missing、metric_missing")
        expectText("第 5 次缺测：Time: final_timing_missing；Prefill: final_timing_missing；Decode: final_timing_missing")
        keepScreenshot("Context speed missing timing")
        click("返回历史任务")
        app.tables["专项历史表"].tableRows.element(boundBy: 2).click()
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
