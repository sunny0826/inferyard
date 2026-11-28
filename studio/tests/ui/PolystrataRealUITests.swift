import XCTest

@MainActor
final class PolystrataRealUITests: XCTestCase {
    var app: XCUIApplication!
    private let baseWindowTitle = "InferYard Studio"

    private var mainWindow: XCUIElement {
        app.windows.matching(NSPredicate(format: "title CONTAINS %@", baseWindowTitle)).firstMatch
    }

    override func setUpWithError() throws {
        continueAfterFailure = false
        let env = ProcessInfo.processInfo.environment
        guard let appPath = env["INFERYARD_POLYSTRATA_UI_APP"],
              let home = env["INFERYARD_POLYSTRATA_UI_HOME"],
              !appPath.isEmpty, !home.isEmpty else {
            throw XCTSkip("Local PolyStrata real UI E2E requires run_polystrata.py")
        }
        app = XCUIApplication(url: URL(fileURLWithPath: appPath))
        app.launchEnvironment["HOME"] = home
        app.launchEnvironment["MYGO_LOCALE"] = "zh-Hans"
        if let pythonPath = env["INFERYARD_POLYSTRATA_UI_PYTHONPATH"], !pythonPath.isEmpty {
            app.launchEnvironment["PYTHONPATH"] = pythonPath
        }
        app.launch()
        app.activate()
        expectExists(mainWindow, timeout: 20)
        XCTAssertFalse(app.windows["InferYard Studio · UI 调试"].exists)
    }

    override func tearDownWithError() throws {
        if let app, app.state != .notRunning {
            if let testRun, testRun.totalFailureCount > 0 {
                if mainWindow.exists {
                    keepScreenshot("Failed real PolyStrata window")
                }
                let tree = XCTAttachment(string: app.debugDescription)
                tree.name = "Accessibility tree"
                tree.lifetime = .keepAlways
                add(tree)
            }
            app.terminate()
        }
    }

    func testRealPolystrataStudioLifecycle() {
        let expectedModel = ProcessInfo.processInfo.environment["INFERYARD_POLYSTRATA_UI_MODEL"] ?? ""

        // 1. Library: verify Strata Pack model registration, no MTP GGUF leak, and engine eligibility.
        click("模型库")
        expectText("已安装的模型", timeout: 20)
        if !expectedModel.isEmpty {
            expectText("\(expectedModel) · PolyStrata", timeout: 20)
        }
        XCTAssertFalse(
            anyTextContains("mtp-"),
            "Auxiliary MTP GGUF under polystrata-data/mtp must not leak as a standalone model"
        )
        clickSectionSwitch("引擎", timeout: 10)
        expectText("已安装的引擎", timeout: 15)
        wait("PolyStrata engine is verified", timeout: 60) {
            self.anyTextContains("PolyStrata") && self.textExists("已核验")
        }
        keepScreenshot("1-Library-Engines-Verified")

        // 2. Services: launch standalone PolyStrata service, probe /v1/chat/completions, and stop cleanly.
        click("服务")
        expectText("托管服务", timeout: 15)
        click("+ 启动模型", timeout: 20)
        expectText("启动模型服务", timeout: 10)
        expectText("内存余量预估", timeout: 10)
        XCTAssertFalse(
            anyTextContains("可用内存不足"),
            "PolyStrata strata_pack resident memory estimate must not falsely block launch"
        )
        click("启动服务", timeout: 10)
        let runningSidebarStatus = app.buttons.matching(
            NSPredicate(format: "title == %@ OR label == %@", "服务运行中", "服务运行中")
        ).firstMatch
        wait("Managed PolyStrata service finishes loading and reaches running state", timeout: 300) {
            if self.textExists("托管服务启动失败") || self.textExists("服务意外退出") || self.textExists("服务异常退出") {
                XCTFail("Managed PolyStrata service failed to start")
                return true
            }
            return (runningSidebarStatus.exists || self.anyTextContains("服务运行中"))
                && !self.textExists("启动中")
                && !self.anyTextContains("服务启动中")
        }
        keepScreenshot("2-Services-Running")

        click("调用示例", timeout: 15)
        expectText("连通性测试", timeout: 10)
        click("发一条测试请求", timeout: 15)
        let okLine = app.descendants(matching: .any).matching(
            NSPredicate(format: "label BEGINSWITH %@ OR title BEGINSWITH %@", "HTTP 200 · 延迟 ", "HTTP 200 · 延迟 ")
        ).firstMatch
        let replyLine = app.descendants(matching: .any).matching(
            NSPredicate(format: "label BEGINSWITH %@ OR title BEGINSWITH %@", "回复：", "回复：")
        ).firstMatch
        wait("Connectivity test returns HTTP 200 and non-empty reply", timeout: 120) {
            if self.anyTextContains("请求失败：") {
                XCTFail("Connectivity test failed on running PolyStrata service")
                return true
            }
            guard okLine.exists && replyLine.exists else { return false }
            let raw = replyLine.label.isEmpty ? replyLine.title : replyLine.label
            let body = raw.replacingOccurrences(of: "回复：", with: "").trimmingCharacters(in: .whitespacesAndNewlines)
            return !body.isEmpty
        }
        keepScreenshot("3-Services-Connectivity-OK")

        closeDrawer(closeLabel: "关闭调用示例", markerText: "连通性测试")

        wait("Managed PolyStrata card reports real multi-GiB worker memory", timeout: 15) {
            !self.anyTextContains("0.0 GiB") && !self.anyTextContains("0.1 GiB")
        }

        click("停止", timeout: 15)
        click("确认停止", timeout: 15)
        wait("Managed PolyStrata service stops cleanly", timeout: 60) {
            self.anyTextContains("当前没有运行中的托管服务")
                && !self.textExists("停止中")
                && !self.anyTextContains("服务停止中")
        }

        // 3. Benchmarks: run real single-bundle (zh-svg-pelican) benchmark and inspect generated report.
        click("测评")
        let newBenchBtn = app.buttons.matching(
            NSPredicate(format: "title == %@ OR label == %@", "新建测评", "新建测评")
        ).firstMatch
        let startBtn = app.buttons.matching(
            NSPredicate(format: "title == %@ OR label == %@", "开始测评", "开始测评")
        ).firstMatch
        wait("Benchmarks page renders", timeout: 15) {
            newBenchBtn.exists || startBtn.exists
        }
        if newBenchBtn.exists {
            click("新建测评", timeout: 15)
        }
        wait("Benchmark configuration stage is ready", timeout: 60) {
            startBtn.exists && startBtn.isEnabled
        }
        let reasoningMenu = app.menuButtons["思考强度"].firstMatch
        expectExists(reasoningMenu, timeout: 10)
        revealIfNeeded(reasoningMenu)
        reasoningMenu.click()
        let lowOption = app.menuItems["低"].firstMatch
        expectExists(lowOption, timeout: 5)
        lowOption.click()
        wait("Launch summary reflects selected thinking effort", timeout: 5) {
            self.anyTextContains("思考强度：低")
        }
        let zhCore = app.checkBoxes["zh-core"].firstMatch
        let zhPelican = app.checkBoxes["zh-svg-pelican"].firstMatch
        expectExists(zhCore, timeout: 10)
        expectExists(zhPelican, timeout: 10)
        if String(describing: zhCore.value ?? "") == "1" {
            wait("zh-core checkbox becomes hittable", timeout: 10) {
                self.revealIfNeeded(zhCore)
                if zhCore.isHittable { return true }
                self.dismissFloatingNotificationsIfPresent()
                return zhCore.isHittable
            }
            zhCore.click()
        }
        wait("Only zh-svg-pelican remains selected", timeout: 5) {
            String(describing: zhCore.value ?? "") == "0"
                && String(describing: zhPelican.value ?? "") == "1"
        }
        keepScreenshot("4-Benchmark-Configured")

        click("开始测评", timeout: 15)

        let abortedLine = app.descendants(matching: .any).matching(
            NSPredicate(format: "label BEGINSWITH %@ OR title BEGINSWITH %@", "当前任务已中止：", "当前任务已中止：")
        ).firstMatch
        let viewReportBtn = app.buttons["查看报告"].firstMatch
        wait("Real PolyStrata benchmark finishes and exposes View Report", timeout: 1200) {
            if abortedLine.exists {
                let msg = abortedLine.label.isEmpty ? abortedLine.title : abortedLine.label
                XCTFail("Real PolyStrata benchmark aborted: \(msg)")
                return true
            }
            return viewReportBtn.exists && viewReportBtn.isEnabled
        }
        click("查看报告", timeout: 20)
        expectText("各类任务表现", timeout: 30)
        expectExists(app.buttons["返回报告列表"].firstMatch, timeout: 15)
        keepScreenshot("5-Benchmark-Report")
    }

    private func dismissFloatingNotificationsIfPresent() {
        let dismissPred = NSPredicate(
            format: "title BEGINSWITH %@ OR label BEGINSWITH %@", "关闭通知：", "关闭通知："
        )
        for _ in 0..<3 {
            let dismissBtn = app.buttons.matching(dismissPred).firstMatch
            guard dismissBtn.exists && dismissBtn.isHittable else { break }
            dismissBtn.click()
        }
    }

    private func closeDrawer(closeLabel: String, markerText: String) {
        let closeBtn = app.buttons.matching(
            NSPredicate(format: "title == %@ OR label == %@", closeLabel, closeLabel)
        ).firstMatch
        if closeBtn.exists && closeBtn.isHittable {
            closeBtn.click()
        } else {
            app.typeKey(.escape, modifierFlags: [])
        }
        wait("Drawer closes (\(markerText))", timeout: 10) {
            !self.textExists(markerText)
        }
    }

    private func revealIfNeeded(_ element: XCUIElement) {
        let scroll = mainWindow.children(matching: .scrollView).element(boundBy: 1)
        guard scroll.exists else { return }
        for _ in 0..<6 {
            let viewport = scroll.frame
            let frame = element.frame
            if frame.isEmpty || viewport.isEmpty { return }
            if frame.minY >= viewport.minY && frame.maxY <= viewport.maxY {
                return
            }
            let delta = frame.maxY > viewport.maxY ? viewport.maxY - frame.maxY : viewport.minY - frame.minY
            scroll.scroll(byDeltaX: 0, deltaY: delta.rounded(.awayFromZero))
        }
    }

    private func click(_ label: String, timeout: TimeInterval = 15) {
        let button = app.buttons.matching(
            NSPredicate(format: "title == %@ OR label == %@", label, label)
        ).firstMatch
        wait("Button \(label) exists, is enabled, and becomes hittable", timeout: timeout) {
            guard button.exists && button.isEnabled else { return false }
            self.revealIfNeeded(button)
            if button.isHittable { return true }
            self.dismissFloatingNotificationsIfPresent()
            return button.isHittable
        }
        button.click()
    }

    private func clickSectionSwitch(_ name: String, timeout: TimeInterval = 10) {
        let titleOrLabel = NSPredicate(format: "title == %@ OR label == %@", name, name)
        let textPred = NSPredicate(format: "label == %@ OR value == %@", name, name)
        let candidates = [
            app.tabs.matching(titleOrLabel).firstMatch,
            app.buttons.matching(titleOrLabel).firstMatch,
            app.radioButtons.matching(titleOrLabel).firstMatch,
            app.otherElements.matching(titleOrLabel).firstMatch,
            app.staticTexts.matching(textPred).firstMatch,
        ]
        var target = candidates[0]
        wait("Section switch \(name) exists", timeout: timeout) {
            for candidate in candidates where candidate.exists {
                self.revealIfNeeded(candidate)
                if !candidate.isHittable {
                    self.dismissFloatingNotificationsIfPresent()
                }
                if candidate.isHittable {
                    target = candidate
                    return true
                }
            }
            return false
        }
        target.click()
    }

    private func textExists(_ value: String) -> Bool {
        app.descendants(matching: .any).matching(
            NSPredicate(format: "label == %@ OR value == %@", value, value)
        ).firstMatch.exists
    }

    private func anyTextContains(_ substring: String) -> Bool {
        app.descendants(matching: .any).matching(
            NSPredicate(format: "label CONTAINS[c] %@ OR title CONTAINS[c] %@", substring, substring)
        ).firstMatch.exists
    }

    private func expectText(_ value: String, timeout: TimeInterval = 10) {
        wait("Missing text: \(value)", timeout: timeout) { self.textExists(value) }
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
            RunLoop.current.run(until: Date().addingTimeInterval(min(0.2, remaining)))
        }
    }

    private func keepScreenshot(_ name: String) {
        guard mainWindow.exists else { return }
        let attachment = XCTAttachment(screenshot: mainWindow.screenshot())
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
    }
}
