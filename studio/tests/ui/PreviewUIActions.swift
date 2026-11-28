import XCTest

@MainActor
extension PreviewUITests {
    func selectScene(_ label: String) {
        if currentScene == label {
            click("重置场景")
            return
        }
        app.popUpButtons["模拟场景"].click()
        let list = app.scrollViews["模拟场景列表"]
        expectExists(list, timeout: 5)
        let option = list.staticTexts[label].firstMatch
        for _ in 0..<16 {
            let viewport = list.frame
            let frame = option.frame
            if !frame.isEmpty && viewport.contains(frame) {
                option.click()
                wait("scene selector closes", timeout: 3) { !list.exists }
                currentScene = label
                return
            }
            list.scroll(byDeltaX: 0, deltaY: frame.midY < viewport.midY ? viewport.height / 2 : -viewport.height / 2)
        }
        XCTFail("Scene option could not be scrolled into view: \(label), option: \(option.frame), list: \(list.frame)")
    }

    private var contentScrollView: XCUIElement {
        app.windows["InferYard Studio · UI 调试"].children(matching: .scrollView).element(boundBy: 1)
    }

    func revealInContent(_ element: XCUIElement) {
        let content = contentScrollView
        for _ in 0..<5 {
            let viewport = content.frame
            let frame = element.frame
            if !frame.isEmpty && viewport.contains(frame) { return }
            let delta = frame.maxY > viewport.maxY ? viewport.maxY - frame.maxY : viewport.minY - frame.minY
            content.scroll(byDeltaX: 0, deltaY: delta.rounded(.awayFromZero))
        }
        XCTAssertTrue(element.isHittable, "The control must be operable after scrolling")
    }

    /// expandFold opens a disclosure that is part of the page rather than a
    /// card, such as the resource sample table.
    func expandFold(_ label: String) {
        let triangle = app.disclosureTriangles[label]
        if triangle.exists {
            revealInContent(triangle)
            triangle.click()
            return
        }
        click(label)
    }

    func keepScreenshot(_ name: String) {
        let attachment = XCTAttachment(screenshot: app.windows["InferYard Studio · UI 调试"].screenshot())
        attachment.name = name
        attachment.lifetime = .keepAlways
        add(attachment)
    }

    /// expectBundleLabels checks the labeled evaluation-set group and each
    /// set's description.
    func expectBundleLabels() {
        expectText("测评题集")
        expectText("· 中文基础能力")
        expectText("· SVG 绘图专项")
    }

    /// prepareSpeed chooses the specialty benchmark, a segmented switch that
    /// stays one click away instead of a dropdown.
    func prepareSpeed() {
        selectTab("长上下文速度")
        expectText("zh-context-speed · 5 次测量 · 每次输出 256 tokens")
    }

    /// expectFirstUseGuide checks the preparation card's milestones and its
    /// settings shortcut.
    func expectFirstUseGuide() {
        expectText("首次使用流程：1. 准备运行环境 → 2. 获取引擎与模型 → 3. 开始首次测评")
        expectExists(app.buttons["前往设置"], timeout: 5)
    }

    func prepare() {
        click("准备环境")
        wait("benchmark becomes available", timeout: 10) {
            self.app.buttons["开始测评"].exists && self.app.buttons["开始测评"].isEnabled
        }
    }

    /// chooseEngine picks one entry from the task page's 引擎 dropdown, which
    /// unlocks and filters 测评模型 to compatible models.
    func chooseEngine(_ label: String) {
        let trigger = app.menuButtons["引擎"].firstMatch
        expectExists(trigger, timeout: 10)
        revealInContent(trigger)
        trigger.click()
        let item = app.menuItems[label].firstMatch
        expectExists(item, timeout: 5)
        item.click()
        wait("engine selection closes", timeout: 5) { !item.exists }
    }

    /// chooseModel picks one entry from the task page's 测评模型 dropdown.
    /// Requires a usable engine to be selected first.
    func chooseModel(_ label: String) {
        let trigger = app.menuButtons["测评模型"].firstMatch
        expectExists(trigger, timeout: 10)
        revealInContent(trigger)
        trigger.click()
        let item = app.menuItems[label].firstMatch
        expectExists(item, timeout: 5)
        item.click()
        wait("model selection closes", timeout: 5) { !item.exists }
    }

    func openMenu(_ label: String) {
        let button = app.menuButtons[label].firstMatch
        wait("menu button \(label) is enabled", timeout: 5) { button.exists && button.isEnabled }
        button.click()
    }

    func click(_ label: String, timeout: TimeInterval = 5) {
        let contentButton = contentScrollView.buttons[label].firstMatch
        let button = contentButton.exists ? contentButton : app.buttons[label].firstMatch
        wait("button \(label) is enabled", timeout: timeout) {
            button.exists && button.isEnabled && (contentButton.exists || button.isHittable)
        }
        if contentButton.exists {
            revealInContent(button)
        }
        button.click()
    }

    /// selectTab chooses a page tab. MyGo exports RoleTab as an AXTabButton, so
    /// macOS reports a `Tab` element whose name is its title, not a button or a
    /// radio button. Callers assert the chosen tab's content, which proves the
    /// choice took effect.
    func selectTab(_ name: String, timeout: TimeInterval = 5) {
        let tab = waitForTab(name, timeout: timeout)
        if !tab.isHittable {
            revealInContent(tab)
        }
        wait("Tab \(name) is hittable", timeout: timeout) { tab.exists && tab.isHittable }
        tab.click()
    }

    /// expectTab waits for a page tab, which expectText cannot find because a
    /// tab's name is its title and its value is only the chosen flag.
    func expectTab(_ name: String, timeout: TimeInterval = 5) {
        _ = waitForTab(name, timeout: timeout)
    }

    /// waitForTab polls both tab roles until one appears and returns it. A tab
    /// can render a few frames after its page data loads, so choosing a query
    /// once would watch the wrong one and time out even after the tab arrived;
    /// a host may also export the same tab as a radio button.
    @discardableResult
    func waitForTab(_ name: String, timeout: TimeInterval) -> XCUIElement {
        let predicate = NSPredicate(format: "title == %@ OR label == %@", name, name)
        let tabElement = app.tabs.matching(predicate).firstMatch
        let radioElement = app.radioButtons.matching(predicate).firstMatch
        var found = tabElement
        wait("Missing tab: \(name)", timeout: timeout) {
            if tabElement.exists {
                found = tabElement
                return true
            }
            if radioElement.exists {
                found = radioElement
                return true
            }
            return false
        }
        return found
    }

    func expectText(_ text: String, timeout: TimeInterval = 10) {
        let element = self.text(text)
        wait("Missing text: \(text)", timeout: timeout) { element.exists }
    }

    func expectExists(_ element: XCUIElement, timeout: TimeInterval) {
        wait("Element must exist", timeout: timeout) { element.exists }
    }

    func text(_ value: String) -> XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "label == %@ OR value == %@", value, value)).firstMatch
    }

    func wait(_ description: String, timeout: TimeInterval, condition: @escaping () -> Bool) {
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
}
