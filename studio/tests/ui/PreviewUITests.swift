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
            if testRun?.hasSucceeded == false {
                let screenshot = XCTAttachment(screenshot: app.screenshot())
                screenshot.name = "Failed preview window"
                screenshot.lifetime = .keepAlways
                add(screenshot)
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

    private func prepare() {
        click("准备环境")
        wait("benchmark becomes available", timeout: 10) {
            self.app.buttons["运行 benchmark"].exists && self.app.buttons["运行 benchmark"].isEnabled
        }
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
