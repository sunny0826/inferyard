import XCTest

@MainActor
final class ReleaseUITests: XCTestCase {
    var app: XCUIApplication!

    override func setUpWithError() throws {
        continueAfterFailure = false
        let environment = ProcessInfo.processInfo.environment
        guard let appPath = environment["INFERYARD_RELEASE_APP"],
              let home = environment["INFERYARD_RELEASE_HOME"], !appPath.isEmpty, !home.isEmpty else {
            throw XCTSkip("Production package smoke requires run_release.py")
        }
        app = XCUIApplication(url: URL(fileURLWithPath: appPath))
        app.launchEnvironment["HOME"] = home
        app.launch()
        expectExists(app.windows["InferYard Studio"], timeout: 15)
        XCTAssertFalse(app.windows["InferYard Studio · UI 调试"].exists)
    }

    override func tearDownWithError() throws {
        if let app, app.state != .notRunning {
            if let testRun, testRun.totalFailureCount > 0 {
                keepScreenshot("Failed production window")
                let tree = XCTAttachment(string: app.debugDescription)
                tree.name = "Accessibility tree"
                tree.lifetime = .keepAlways
                add(tree)
            }
            app.terminate()
        }
    }

    func testProductionPagesAndRealReport() {
        expectTab("测评报告")
        openRealReport()
        click("模型")
        expectText("已安装的模型")
        click("下载")
        expectText("新建下载任务")
        click("设备")
        expectExists(app.buttons["刷新设备"], timeout: 10)
        click("设置")
        expectText("路径设置")
        for key in ["INFERYARD_RELEASE_CLI", "INFERYARD_RELEASE_RESULTS"] {
            if let value = ProcessInfo.processInfo.environment[key] {
                expectText(value)
            }
        }
        keepScreenshot("Production settings")
        click("报告")
        openRealReport()
    }

    private func openRealReport() {
        let open = app.buttons.matching(NSPredicate(format: "title BEGINSWITH %@", "打开报告 · ")).firstMatch
        expectExists(open, timeout: 20)
        open.click()
        expectText("各类任务表现", timeout: 20)
        expectExists(app.buttons["返回报告列表"], timeout: 10)
        keepScreenshot("Production real report")
        click("返回报告列表")
    }

    private func click(_ label: String) {
        let button = app.buttons[label].firstMatch
        expectExists(button, timeout: 10)
        wait("Button is enabled: \(label)", timeout: 10) { button.isEnabled }
        button.click()
    }

    private func expectText(_ value: String, timeout: TimeInterval = 10) {
        let element = app.descendants(matching: .any).matching(
            NSPredicate(format: "label == %@ OR value == %@", value, value)
        ).firstMatch
        wait("Missing text: \(value)", timeout: timeout) { element.exists }
    }

    /// expectTab waits for a page tab. AppKit exposes the tab as a `Tab`
    /// element whose name is its title, so neither the text matcher (label or
    /// value) nor app.radioButtons finds it. The tab can render a few frames
    /// after its page data loads, so both roles are polled until one appears
    /// instead of choosing a query once.
    private func expectTab(_ name: String, timeout: TimeInterval = 10) {
        let predicate = NSPredicate(format: "title == %@ OR label == %@", name, name)
        let tabElement = app.tabs.matching(predicate).firstMatch
        let radioElement = app.radioButtons.matching(predicate).firstMatch
        wait("Missing tab: \(name)", timeout: timeout) {
            tabElement.exists || radioElement.exists
        }
    }

    private func expectExists(_ element: XCUIElement, timeout: TimeInterval) {
        wait("Element must exist: \(element)", timeout: timeout) { element.exists }
    }

    private func wait(_ description: String, timeout: TimeInterval, condition: @escaping () -> Bool) {
        let deadline = ProcessInfo.processInfo.systemUptime + timeout
        while true {
            if condition() { return }
            let remaining = deadline - ProcessInfo.processInfo.systemUptime
            if remaining <= 0 {
                XCTFail(description)
                return
            }
            RunLoop.current.run(until: Date().addingTimeInterval(min(0.1, remaining)))
        }
    }

    private func keepScreenshot(_ name: String) {
        let attachment = XCTAttachment(screenshot: app.windows["InferYard Studio"].screenshot())
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
    }
}
