import XCTest

@MainActor
final class PolystrataRealUITests: XCTestCase {
    var app: XCUIApplication!
    private let windowTitle = "InferYard Studio"

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
        expectExists(app.windows[windowTitle], timeout: 20)
        XCTAssertFalse(app.windows["InferYard Studio · UI 调试"].exists)
    }

    override func tearDownWithError() throws {
        if let app, app.state != .notRunning {
            if let testRun, testRun.totalFailureCount > 0 {
                if app.windows[windowTitle].exists {
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
        click("+ 启动模型", timeout: 15)
        expectText("启动模型服务", timeout: 10)
        expectText("内存余量预估", timeout: 10)
        XCTAssertFalse(
            anyTextContains("可用内存不足"),
            "PolyStrata strata_pack resident memory estimate must not falsely block launch"
        )
        click("启动服务", timeout: 10)
        wait("Managed PolyStrata service reaches running state", timeout: 300) {
            if self.textExists("托管服务启动失败") || self.textExists("服务意外退出") {
                XCTFail("Managed PolyStrata service failed to start")
                return true
            }
            return self.anyTextContains("1 个托管服务运行中")
        }
        keepScreenshot("2-Services-Running")

        click("调用示例", timeout: 15)
        expectText("连通性测试", timeout: 10)
        click("发一条测试请求", timeout: 10)
        let okLine = app.descendants(matching: .any).matching(
            NSPredicate(format: "label BEGINSWITH %@ OR value BEGINSWITH %@", "HTTP 200 · 延迟 ", "HTTP 200 · 延迟 ")
        ).firstMatch
        let replyLine = app.descendants(matching: .any).matching(
            NSPredicate(format: "label BEGINSWITH %@ OR value BEGINSWITH %@", "回复：", "回复：")
        ).firstMatch
        wait("Connectivity test returns HTTP 200 and non-empty reply", timeout: 120) {
            if self.anyTextContains("请求失败：") {
                XCTFail("Connectivity test failed on running PolyStrata service")
                return true
            }
            guard okLine.exists && replyLine.exists else { return false }
            let raw = (replyLine.value as? String) ?? replyLine.label
            let body = raw.replacingOccurrences(of: "回复：", with: "").trimmingCharacters(in: .whitespacesAndNewlines)
            return !body.isEmpty
        }
        keepScreenshot("3-Services-Connectivity-OK")

        closeDrawer(closeLabel: "关闭调用示例", markerText: "连通性测试")

        click("停止", timeout: 10)
        click("确认停止", timeout: 10)
        wait("Managed PolyStrata service stops cleanly", timeout: 60) {
            self.anyTextContains("当前没有运行中的托管服务")
        }

        // 3. Benchmarks: run real single-bundle (zh-svg-pelican) benchmark and inspect generated report.
        click("测评")
        if app.buttons["新建测评"].exists {
            click("新建测评", timeout: 15)
        }
        let startBtn = app.buttons["开始测评"].firstMatch
        wait("Benchmark configuration stage is ready", timeout: 60) {
            startBtn.exists && startBtn.isEnabled
        }
        let zhCore = app.checkBoxes["zh-core"].firstMatch
        let zhPelican = app.checkBoxes["zh-svg-pelican"].firstMatch
        expectExists(zhCore, timeout: 10)
        expectExists(zhPelican, timeout: 10)
        if String(describing: zhCore.value ?? "") == "1" {
            revealIfNeeded(zhCore)
            zhCore.click()
        }
        wait("Only zh-svg-pelican remains selected", timeout: 5) {
            String(describing: zhCore.value ?? "") == "0"
                && String(describing: zhPelican.value ?? "") == "1"
        }
        keepScreenshot("4-Benchmark-Configured")

        revealIfNeeded(startBtn)
        startBtn.click()

        let abortedLine = app.descendants(matching: .any).matching(
            NSPredicate(format: "label BEGINSWITH %@ OR value BEGINSWITH %@", "当前任务已中止：", "当前任务已中止：")
        ).firstMatch
        let viewReportBtn = app.buttons["查看报告"].firstMatch
        wait("Real PolyStrata benchmark finishes and exposes View Report", timeout: 1200) {
            if abortedLine.exists {
                let msg = (abortedLine.value as? String) ?? abortedLine.label
                XCTFail("Real PolyStrata benchmark aborted: \(msg)")
                return true
            }
            return viewReportBtn.exists && viewReportBtn.isEnabled
        }
        viewReportBtn.click()
        expectText("各类任务表现", timeout: 30)
        expectExists(app.buttons["返回报告列表"].firstMatch, timeout: 15)
        keepScreenshot("5-Benchmark-Report")
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
        if element.isHittable { return }
        let scroll = app.windows[windowTitle].children(matching: .scrollView).element(boundBy: 1)
        guard scroll.exists else { return }
        for _ in 0..<6 {
            if element.isHittable { return }
            let viewport = scroll.frame
            let frame = element.frame
            if frame.isEmpty { break }
            let delta = frame.maxY > viewport.maxY ? viewport.maxY - frame.maxY : viewport.minY - frame.minY
            scroll.scroll(byDeltaX: 0, deltaY: delta.rounded(.awayFromZero))
        }
    }

    private func click(_ label: String, timeout: TimeInterval = 10) {
        let button = app.buttons.matching(
            NSPredicate(format: "title == %@ OR label == %@", label, label)
        ).firstMatch
        wait("Button \(label) exists and is enabled", timeout: timeout) {
            button.exists && button.isEnabled
        }
        revealIfNeeded(button)
        button.click()
    }

    private func clickSectionSwitch(_ name: String, timeout: TimeInterval = 10) {
        let titleOrLabel = NSPredicate(format: "title == %@ OR label == %@", name, name)
        let textPred = NSPredicate(format: "label == %@ OR value == %@", name, name)
        let candidates = [
            app.buttons.matching(titleOrLabel).firstMatch,
            app.tabs.matching(titleOrLabel).firstMatch,
            app.radioButtons.matching(titleOrLabel).firstMatch,
            app.otherElements.matching(titleOrLabel).firstMatch,
            app.staticTexts.matching(textPred).firstMatch,
        ]
        var target = candidates[0]
        wait("Section switch \(name) exists", timeout: timeout) {
            for candidate in candidates where candidate.exists {
                target = candidate
                return true
            }
            return false
        }
        revealIfNeeded(target)
        target.click()
    }

    private func textExists(_ value: String) -> Bool {
        app.descendants(matching: .any).matching(
            NSPredicate(format: "label == %@ OR value == %@", value, value)
        ).firstMatch.exists
    }

    private func anyTextContains(_ substring: String) -> Bool {
        app.descendants(matching: .any).matching(
            NSPredicate(format: "label CONTAINS[c] %@ OR value CONTAINS[c] %@", substring, substring)
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
        guard app.windows[windowTitle].exists else { return }
        let attachment = XCTAttachment(screenshot: app.windows[windowTitle].screenshot())
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
    }
}
