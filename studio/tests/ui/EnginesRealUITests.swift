import XCTest

@MainActor
final class EnginesRealUITests: XCTestCase {
    var app: XCUIApplication!
    private let baseWindowTitle = "InferYard Studio"

    private var mainWindow: XCUIElement {
        app.windows.matching(NSPredicate(format: "title CONTAINS %@", baseWindowTitle)).firstMatch
    }

    override func setUpWithError() throws {
        continueAfterFailure = false
        let env = ProcessInfo.processInfo.environment
        guard let appPath = env["INFERYARD_ENGINES_UI_APP"],
              let home = env["INFERYARD_ENGINES_UI_HOME"],
              !appPath.isEmpty, !home.isEmpty else {
            throw XCTSkip("EnginesRealUITests requires run_engines.py")
        }
        app = XCUIApplication(url: URL(fileURLWithPath: appPath))
        app.launchEnvironment["HOME"] = home
        app.launchEnvironment["MYGO_LOCALE"] = "zh-Hans"
        if let pythonPath = env["INFERYARD_ENGINES_UI_PYTHONPATH"], !pythonPath.isEmpty {
            app.launchEnvironment["PYTHONPATH"] = pythonPath
        }
        for key in ["INFERYARD_ENGINE_UPDATE_GITHUB_API", "INFERYARD_ENGINE_UPDATE_PYPI_API"] {
            if let value = env[key], !value.isEmpty {
                app.launchEnvironment[key] = value
            }
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
                    keepScreenshot("Failed real Engines window")
                }
                let tree = XCTAttachment(string: app.debugDescription)
                tree.name = "Accessibility tree"
                tree.lifetime = .keepAlways
                add(tree)
            }
            app.terminate()
        }
    }

    func testAllSupportedEnginesUpgradeAndRun() {
        let env = ProcessInfo.processInfo.environment
        let rawEngines = env["INFERYARD_ENGINES_UI_ENGINES"] ?? "llama-cpp,mlx-lm,vllm,polystrata"
        let enabled = Set(
            rawEngines.split(separator: ",").map { $0.trimmingCharacters(in: .whitespaces) }
        )
        let polystrataModel = env["INFERYARD_ENGINES_UI_POLYSTRATA_MODEL"] ?? ""

        // 1. Library: verify staged small models, catalog sections, update detection, and one-click upgrades.
        click("模型库")
        expectText("已安装的模型", timeout: 25)
        if enabled.contains("llama-cpp") {
            expectText("SmolLM2-135M-Instruct-Q4_K_M.gguf · GGUF", timeout: 20)
        }
        if enabled.contains("mlx-lm") {
            expectText("smollm2-mlx · 目录模型", timeout: 20)
        }
        if enabled.contains("vllm") {
            expectText("smollm2-hf · 目录模型", timeout: 20)
        }
        if enabled.contains("polystrata") && !polystrataModel.isEmpty {
            expectText("\(polystrataModel) · PolyStrata", timeout: 20)
        }

        clickSectionSwitch("引擎", timeout: 10)
        expectText("已安装的引擎", timeout: 20)
        expectText("可下载引擎", timeout: 15)
        expectText("外部应用", timeout: 15)
        expectText("LM Studio", timeout: 15)
        expectText("SGLang", timeout: 15)

        wait("All staged engines finish initial verification", timeout: 90) {
            self.textExists("已核验")
                && !self.textExists("核验中")
                && !self.anyTextContains("正在核验")
        }

        click("检查引擎更新", timeout: 30)

        let updateTargets: [(family: String, engineName: String, timeout: TimeInterval)] = [
            ("llama-cpp", "llama-prism-b10743 · llama-server", 300),
            ("mlx-lm", "mlx-lm · 0.31.1", 300),
            ("vllm", "vLLM · 0.31.0+cpu", 600),
            ("polystrata", "PolyStrata · v0.2.20-poly.4", 900),
        ]
        for target in updateTargets where enabled.contains(target.family) {
            let btnLabel = "更新 \(target.engineName)"
            wait("One-click update button appears for \(target.family)", timeout: 45) {
                let btn = self.buttonMatching(btnLabel)
                return btn.exists && btn.isEnabled
            }
        }
        keepScreenshot("1-Library-Updates-Detected")

        for target in updateTargets where enabled.contains(target.family) {
            runOneClickUpgrade(
                family: target.family,
                engineName: target.engineName,
                timeout: target.timeout
            )
        }
        keepScreenshot("2-Library-All-Engines-Upgraded")

        // 2. Services: verify standalone launch, live /v1/chat/completions HTTP 200, and clean stop.
        let hasStandaloneTarget =
            enabled.contains("llama-cpp") || enabled.contains("mlx-lm")
            || enabled.contains("vllm") || enabled.contains("polystrata")
        if hasStandaloneTarget {
            click("服务")
            expectText("托管服务", timeout: 15)

            if enabled.contains("llama-cpp") {
                verifyStandaloneService(
                    label: "llama-cpp",
                    modelOption: "SmolLM2-135M-Instruct-Q4_K_M.gguf · GGUF",
                    enginePrefix: "llama-cpp-prism-",
                    excludedEngine: "llama-prism-b10743 · llama-server",
                    startTimeout: 120
                )
            }
            if enabled.contains("mlx-lm") {
                verifyStandaloneService(
                    label: "mlx-lm",
                    modelOption: "smollm2-mlx · 目录模型",
                    enginePrefix: "mlx-lm · ",
                    excludedEngine: "mlx-lm · 0.31.1",
                    startTimeout: 180
                )
            }
            if enabled.contains("vllm") {
                verifyStandaloneService(
                    label: "vllm",
                    modelOption: "smollm2-hf · 目录模型",
                    enginePrefix: "vLLM · ",
                    excludedEngine: "vLLM · 0.31.0+cpu",
                    startTimeout: 240
                )
            }
            if enabled.contains("polystrata") && !polystrataModel.isEmpty {
                verifyStandaloneService(
                    label: "polystrata",
                    modelOption: "\(polystrataModel) · PolyStrata",
                    enginePrefix: "PolyStrata · ",
                    excludedEngine: "PolyStrata · v0.2.20-poly.4",
                    startTimeout: 360
                )
            }
        }

        // 3. Benchmarks: run real zh-svg-pelican benchmark and verify studio.json report view.
        if enabled.contains("mlx-lm") {
            verifyBenchmarkRun(enginePrefix: "mlx-lm · ", excludedEngine: "mlx-lm · 0.31.1", modelOption: "smollm2-mlx · 目录模型", timeout: 300)
        } else if enabled.contains("llama-cpp") {
            verifyBenchmarkRun(enginePrefix: "llama-cpp-prism-", excludedEngine: "llama-prism-b10743 · llama-server", modelOption: "SmolLM2-135M-Instruct-Q4_K_M.gguf · GGUF", timeout: 300)
        } else if enabled.contains("vllm") {
            verifyBenchmarkRun(enginePrefix: "vLLM · ", excludedEngine: "vLLM · 0.31.0+cpu", modelOption: "smollm2-hf · 目录模型", timeout: 420)
        } else if enabled.contains("polystrata") && !polystrataModel.isEmpty {
            verifyBenchmarkRun(enginePrefix: "PolyStrata · ", excludedEngine: "PolyStrata · v0.2.20-poly.4", modelOption: "\(polystrataModel) · PolyStrata", timeout: 1200)
        }
    }

    private func runOneClickUpgrade(family: String, engineName: String, timeout: TimeInterval) {
        let btnLabel = "更新 \(engineName)"
        click(btnLabel, timeout: 20)
        wait("Engine \(family) finishes one-click upgrade and re-verifies", timeout: timeout) {
            if self.anyTextContains("更新失败") || self.anyTextContains("升级失败") || self.anyTextContains("安装失败") {
                XCTFail("One-click upgrade failed for \(family)")
                return true
            }
            let updateBtnStillVisible = self.buttonMatching(btnLabel).exists
            let stillUpdating = self.textExists("更新中…")
            let stillChecking = self.textExists("核验中") || self.anyTextContains("正在核验")
            if updateBtnStillVisible || stillUpdating || stillChecking {
                return false
            }
            if self.textExists("不可用") {
                XCTFail("Upgraded engine \(family) became ineligible (不可用)")
                return true
            }
            if family == "llama-cpp" {
                return self.anyTextContains("llama-cpp-prism-")
            }
            return self.textExists("已核验")
        }
        if family == "polystrata" {
            let unlinkLabel = "移除引擎关联 · \(engineName)"
            if buttonMatching(unlinkLabel).exists {
                click(unlinkLabel, timeout: 15)
                wait("Pre-upgrade PolyStrata installation is unlinked", timeout: 15) {
                    !self.textExists(engineName)
                }
            }
        }
    }

    private func verifyStandaloneService(
        label: String,
        modelOption: String,
        enginePrefix: String,
        excludedEngine: String?,
        startTimeout: TimeInterval
    ) {
        click("+ 启动模型", timeout: 20)
        expectText("启动模型服务", timeout: 10)
        chooseMenuOption(menuLabel: "模型", exactItem: modelOption, timeout: 15)
        chooseMenuPrefixOption(
            menuLabel: "引擎",
            prefix: enginePrefix,
            excluding: excludedEngine,
            timeout: 20
        )
        click("启动服务", timeout: 15)

        let runningSidebarStatus = buttonMatching("服务运行中")
        wait("Managed \(label) service reaches running state", timeout: startTimeout) {
            if self.textExists("托管服务启动失败") || self.textExists("服务意外退出") || self.textExists("服务异常退出") {
                XCTFail("Managed \(label) service failed to start")
                return true
            }
            return (runningSidebarStatus.exists || self.anyTextContains("服务运行中"))
                && !self.textExists("启动中")
                && !self.anyTextContains("服务启动中")
        }
        keepScreenshot("3-Services-\(label)-Running")

        click("调用示例", timeout: 15)
        expectText("连通性测试", timeout: 10)
        click("发一条测试请求", timeout: 15)
        let okLine = app.descendants(matching: .any).matching(
            NSPredicate(format: "label BEGINSWITH %@ OR value BEGINSWITH %@", "HTTP 200 · 延迟 ", "HTTP 200 · 延迟 ")
        ).firstMatch
        let replyLine = app.descendants(matching: .any).matching(
            NSPredicate(format: "label BEGINSWITH %@ OR value BEGINSWITH %@", "回复：", "回复：")
        ).firstMatch
        wait("Connectivity test on \(label) returns HTTP 200 and non-empty reply", timeout: 120) {
            if self.anyTextContains("请求失败：") {
                XCTFail("Connectivity test failed on running \(label) service")
                return true
            }
            guard okLine.exists && replyLine.exists else { return false }
            let raw = (replyLine.value as? String) ?? replyLine.label
            let body = raw.replacingOccurrences(of: "回复：", with: "")
                .trimmingCharacters(in: .whitespacesAndNewlines)
            return !body.isEmpty
        }
        closeDrawer(closeLabel: "关闭调用示例", markerText: "连通性测试")

        click("停止", timeout: 15)
        click("确认停止", timeout: 15)
        wait("Managed \(label) service stops cleanly", timeout: 60) {
            self.anyTextContains("当前没有运行中的托管服务")
                && !self.textExists("停止中")
                && !self.anyTextContains("服务停止中")
        }
    }

    private func verifyBenchmarkRun(
        enginePrefix: String,
        excludedEngine: String?,
        modelOption: String,
        timeout: TimeInterval
    ) {
        click("测评")
        let newBenchBtn = buttonMatching("新建测评")
        let engineMenu = app.menuButtons["引擎"].firstMatch
        wait("Benchmarks page renders", timeout: 20) {
            newBenchBtn.exists || engineMenu.exists
        }
        if newBenchBtn.exists {
            click("新建测评", timeout: 15)
        }
        expectExists(engineMenu, timeout: 30)
        chooseMenuPrefixOption(
            menuLabel: "引擎",
            prefix: enginePrefix,
            excluding: excludedEngine,
            timeout: 20
        )
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
        keepScreenshot("4-Benchmark-Configured")
        click("开始测评", timeout: 15)

        let abortedLine = app.descendants(matching: .any).matching(
            NSPredicate(format: "label BEGINSWITH %@ OR value BEGINSWITH %@", "当前任务已中止：", "当前任务已中止：")
        ).firstMatch
        let viewReportBtn = app.buttons["查看报告"].firstMatch
        wait("Real benchmark finishes and exposes View Report", timeout: timeout) {
            if abortedLine.exists {
                let msg = (abortedLine.value as? String) ?? abortedLine.label
                XCTFail("Real benchmark aborted: \(msg)")
                return true
            }
            return viewReportBtn.exists && viewReportBtn.isEnabled
        }
        click("查看报告", timeout: 20)
        expectText("各类任务表现", timeout: 30)
        expectExists(app.buttons["返回报告列表"].firstMatch, timeout: 15)
        keepScreenshot("5-Benchmark-Report")
    }

    private func chooseMenuOption(menuLabel: String, exactItem: String, timeout: TimeInterval) {
        let menu = app.menuButtons[menuLabel].firstMatch
        wait("Menu \(menuLabel) exists and is enabled", timeout: timeout) {
            menu.exists && menu.isEnabled
        }
        revealIfNeeded(menu)
        if !menu.isHittable {
            dismissFloatingNotificationsIfPresent()
        }
        menu.click()
        let item = app.menuItems[exactItem].firstMatch
        expectExists(item, timeout: 10)
        item.click()
    }

    private func chooseMenuPrefixOption(
        menuLabel: String,
        prefix: String,
        excluding: String?,
        timeout: TimeInterval
    ) {
        let menu = app.menuButtons[menuLabel].firstMatch
        wait("Menu \(menuLabel) exists and is enabled", timeout: timeout) {
            menu.exists && menu.isEnabled
        }
        revealIfNeeded(menu)
        if !menu.isHittable {
            dismissFloatingNotificationsIfPresent()
        }
        menu.click()
        let pred: NSPredicate
        if let excluding, !excluding.isEmpty {
            pred = NSPredicate(format: "title BEGINSWITH %@ AND title != %@", prefix, excluding)
        } else {
            pred = NSPredicate(format: "title BEGINSWITH %@", prefix)
        }
        let item = app.menuItems.matching(pred).firstMatch
        expectExists(item, timeout: 10)
        item.click()
    }

    private func buttonMatching(_ label: String) -> XCUIElement {
        app.buttons.matching(NSPredicate(format: "title == %@ OR label == %@", label, label)).firstMatch
    }

    private func dismissFloatingNotificationsIfPresent() {
        let dismissPred = NSPredicate(
            format: "title BEGINSWITH %@ OR label BEGINSWITH %@", "关闭通知：", "关闭通知："
        )
        for _ in 0..<4 {
            let dismissBtn = app.buttons.matching(dismissPred).firstMatch
            guard dismissBtn.exists && dismissBtn.isHittable else { break }
            dismissBtn.click()
        }
    }

    private func closeDrawer(closeLabel: String, markerText: String) {
        let closeBtn = buttonMatching(closeLabel)
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
            .matching(NSPredicate(format: "label CONTAINS[c] %@ OR value CONTAINS[c] %@", substring, substring))
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
