import XCTest

@MainActor
final class BonsaiRealUITests: XCTestCase {
    var app: XCUIApplication!
    private let baseWindowTitle = "InferYard Studio"

    private var mainWindow: XCUIElement {
        app.windows.matching(NSPredicate(format: "title CONTAINS %@", baseWindowTitle)).firstMatch
    }

    override func setUpWithError() throws {
        continueAfterFailure = false
        let env = ProcessInfo.processInfo.environment
        guard let appPath = env["INFERYARD_BONSAI_UI_APP"],
              let home = env["INFERYARD_BONSAI_UI_HOME"],
              !appPath.isEmpty, !home.isEmpty else {
            throw XCTSkip("Local Bonsai real UI E2E requires run_bonsai.py")
        }
        app = XCUIApplication(url: URL(fileURLWithPath: appPath))
        app.launchEnvironment["HOME"] = home
        app.launchEnvironment["MYGO_LOCALE"] = "zh-Hans"
        if let pythonPath = env["INFERYARD_BONSAI_UI_PYTHONPATH"], !pythonPath.isEmpty {
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
                    keepScreenshot("Failed real Bonsai window")
                }
                let tree = XCTAttachment(string: app.debugDescription)
                tree.name = "Accessibility tree"
                tree.lifetime = .keepAlways
                add(tree)
            }
            app.terminate()
        }
    }

    func testRealBonsaiStudioLifecycle() {
        let env = ProcessInfo.processInfo.environment
        let chosenModel = env["INFERYARD_BONSAI_UI_MODEL"] ?? ""
        XCTAssertFalse(chosenModel.isEmpty, "INFERYARD_BONSAI_UI_MODEL must be set")
        let allModels = (env["INFERYARD_BONSAI_UI_ALL_MODELS"] ?? chosenModel)
            .split(separator: ",")
            .map { $0.trimmingCharacters(in: .whitespaces) }
            .filter { !$0.isEmpty }
        let enginePrefix = env["INFERYARD_BONSAI_UI_ENGINE_PREFIX"] ?? "llama-cpp-prism-"
        let engineHint = enginePrefix.replacingOccurrences(of: " · ", with: "")
        let modelOption = "\(chosenModel) · GGUF"

        // 1. Library: verify staged Bonsai models and PrismML llama-cpp engine eligibility.
        click("模型库")
        expectText("已安装的模型", timeout: 25)
        for modelFile in allModels {
            expectText("\(modelFile) · GGUF", timeout: 25)
        }

        clickSectionSwitch("引擎", timeout: 10)
        expectText("已安装的引擎", timeout: 15)
        wait("PrismML llama-cpp engine is verified", timeout: 90) {
            self.anyTextContains(engineHint)
                && self.textExists("已核验")
                && !self.textExists("核验中")
                && !self.anyTextContains("正在核验")
        }
        keepScreenshot("1-Library-Bonsai-Verified")

        // 2. Services: launch standalone Bonsai service, probe /v1/chat/completions HTTP 200, stop cleanly.
        click("服务")
        expectText("托管服务", timeout: 15)
        click("+ 启动模型", timeout: 20)
        expectText("启动模型服务", timeout: 10)
        chooseMenuOption(menuLabel: "模型", exactItem: modelOption, timeout: 20)
        chooseMenuPrefixOption(menuLabel: "引擎", prefix: enginePrefix, timeout: 20)
        expectText("内存余量预估", timeout: 10)
        XCTAssertFalse(
            anyTextContains("可用内存不足"),
            "Bonsai PTQ1_0/PQ2_0 resident memory estimate must not falsely block launch"
        )
        click("启动服务", timeout: 15)

        let runningSidebarStatus = buttonMatching("服务运行中")
        wait("Managed Bonsai service reaches running state", timeout: 180) {
            if self.textExists("托管服务启动失败") || self.textExists("服务意外退出") || self.textExists("服务异常退出") {
                XCTFail("Managed Bonsai service failed to start")
                return true
            }
            return (runningSidebarStatus.exists || self.anyTextContains("服务运行中"))
                && !self.textExists("启动中")
                && !self.anyTextContains("服务启动中")
        }
        keepScreenshot("2-Services-Bonsai-Running")

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
                XCTFail("Connectivity test failed on running Bonsai service")
                return true
            }
            guard okLine.exists && replyLine.exists else { return false }
            let raw = replyLine.label.isEmpty ? replyLine.title : replyLine.label
            let body = raw.replacingOccurrences(of: "回复：", with: "")
                .trimmingCharacters(in: .whitespacesAndNewlines)
            return !body.isEmpty
        }
        keepScreenshot("3-Services-Bonsai-Connectivity-OK")
        closeDrawer(closeLabel: "关闭调用示例", markerText: "连通性测试")

        wait("Managed Bonsai card reports real multi-GiB memory", timeout: 15) {
            !self.anyTextContains("0.0 GiB") && !self.anyTextContains("0.1 GiB")
        }

        click("停止", timeout: 15)
        click("确认停止", timeout: 15)
        wait("Managed Bonsai service stops cleanly", timeout: 60) {
            self.anyTextContains("当前没有运行中的托管服务")
                && !self.textExists("停止中")
                && !self.anyTextContains("服务停止中")
        }

        // 3. Benchmarks: run real zh-svg-pelican benchmark and inspect generated studio.json report.
        click("测评")
        let newBenchBtn = buttonMatching("新建测评")
        let engineMenu = app.menuButtons["引擎"].firstMatch
        wait("Benchmarks page renders", timeout: 20) {
            newBenchBtn.exists || engineMenu.exists
        }
        if newBenchBtn.exists {
            click("新建测评", timeout: 15)
        }
        wait("Engine dropdown is ready", timeout: 30) {
            engineMenu.exists && engineMenu.isEnabled
        }
        let modelMenu = app.menuButtons["测评模型"].firstMatch
        if modelMenu.exists {
            XCTAssertFalse(modelMenu.isEnabled, "Model dropdown must stay disabled until an engine is selected")
        }
        chooseMenuPrefixOption(menuLabel: "引擎", prefix: enginePrefix, timeout: 20)
        chooseMenuOption(menuLabel: "测评模型", exactItem: modelOption, timeout: 15)

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
        let startBtn = buttonMatching("开始测评")
        wait("Start Benchmark button is enabled", timeout: 20) {
            startBtn.exists && startBtn.isEnabled
        }
        keepScreenshot("4-Benchmark-Bonsai-Configured")
        click("开始测评", timeout: 15)

        let abortedLine = app.descendants(matching: .any).matching(
            NSPredicate(format: "label BEGINSWITH %@ OR title BEGINSWITH %@", "当前任务已中止：", "当前任务已中止：")
        ).firstMatch
        let viewReportBtn = app.buttons["查看报告"].firstMatch
        wait("Real Bonsai benchmark finishes and exposes View Report", timeout: 600) {
            if abortedLine.exists {
                let msg = abortedLine.label.isEmpty ? abortedLine.title : abortedLine.label
                XCTFail("Real Bonsai benchmark aborted: \(msg)")
                return true
            }
            return viewReportBtn.exists && viewReportBtn.isEnabled
        }
        click("查看报告", timeout: 20)
        expectText("各类任务表现", timeout: 30)
        expectExists(app.buttons["返回报告列表"].firstMatch, timeout: 15)
        keepScreenshot("5-Benchmark-Bonsai-Report")
    }

    private func chooseMenuOption(menuLabel: String, exactItem: String, timeout: TimeInterval) {
        let menu = app.menuButtons[menuLabel].firstMatch
        wait("Menu \(menuLabel) exists and is enabled", timeout: timeout) {
            menu.exists && menu.isEnabled
        }
        revealIfNeeded(menu)
        if !menu.isHittable { dismissFloatingNotificationsIfPresent() }
        menu.click()
        let item = app.menuItems[exactItem].firstMatch
        expectExists(item, timeout: 10)
        item.click()
    }

    private func chooseMenuPrefixOption(menuLabel: String, prefix: String, timeout: TimeInterval) {
        let menu = app.menuButtons[menuLabel].firstMatch
        wait("Menu \(menuLabel) exists and is enabled", timeout: timeout) {
            menu.exists && menu.isEnabled
        }
        revealIfNeeded(menu)
        if !menu.isHittable { dismissFloatingNotificationsIfPresent() }
        menu.click()
        let item = app.menuItems.matching(NSPredicate(format: "title BEGINSWITH %@", prefix)).firstMatch
        expectExists(item, timeout: 10)
        item.click()
    }

    private func buttonMatching(_ label: String) -> XCUIElement {
        app.buttons.matching(NSPredicate(format: "title == %@ OR label == %@", label, label)).firstMatch
    }

    private func dismissFloatingNotificationsIfPresent() {
        let pred = NSPredicate(format: "title BEGINSWITH %@ OR label BEGINSWITH %@", "关闭通知：", "关闭通知：")
        for _ in 0..<4 {
            let btn = app.buttons.matching(pred).firstMatch
            guard btn.exists && btn.isHittable else { break }
            btn.click()
        }
    }

    private func closeDrawer(closeLabel: String, markerText: String) {
        let closeBtn = buttonMatching(closeLabel)
        if closeBtn.exists && closeBtn.isHittable {
            closeBtn.click()
        } else {
            app.typeKey(.escape, modifierFlags: [])
        }
        wait("Drawer closes (\(markerText))", timeout: 10) { !self.textExists(markerText) }
    }

    private func revealIfNeeded(_ element: XCUIElement) {
        let scroll = mainWindow.children(matching: .scrollView).element(boundBy: 1)
        guard scroll.exists else { return }
        for _ in 0..<6 {
            let viewport = scroll.frame
            let frame = element.frame
            if frame.isEmpty || viewport.isEmpty { return }
            if frame.minY >= viewport.minY && frame.maxY <= viewport.maxY { return }
            let delta = frame.maxY > viewport.maxY ? viewport.maxY - frame.maxY : viewport.minY - frame.minY
            scroll.scroll(byDeltaX: 0, deltaY: delta.rounded(.awayFromZero))
        }
    }

    private func click(_ label: String, timeout: TimeInterval = 15) {
        let button = buttonMatching(label)
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
                if !candidate.isHittable { self.dismissFloatingNotificationsIfPresent() }
                if candidate.isHittable { target = candidate; return true }
            }
            return false
        }
        target.click()
    }

    private func textExists(_ value: String) -> Bool {
        app.descendants(matching: .any)
            .matching(NSPredicate(format: "label == %@ OR value == %@", value, value)).firstMatch.exists
    }

    private func anyTextContains(_ substring: String) -> Bool {
        app.descendants(matching: .any)
            .matching(NSPredicate(format: "label CONTAINS[c] %@ OR title CONTAINS[c] %@", substring, substring))
            .firstMatch.exists
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
            if remaining <= 0 { XCTFail(description); return }
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
