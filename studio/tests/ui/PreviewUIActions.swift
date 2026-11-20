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
        for _ in 0..<8 {
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

    func revealInContent(_ element: XCUIElement) {
        let content = app.scrollViews.element(boundBy: 1)
        for _ in 0..<5 {
            let viewport = content.frame
            let frame = element.frame
            if !frame.isEmpty && viewport.contains(frame) { return }
            let delta = frame.maxY > viewport.maxY ? viewport.maxY - frame.maxY : viewport.minY - frame.minY
            content.scroll(byDeltaX: 0, deltaY: delta.rounded(.awayFromZero))
        }
        XCTAssertTrue(element.isHittable, "The control must be operable after scrolling")
    }

    /// taskCard scopes fold lookups to one card so a label shared by several
    /// cards never targets the wrong task.
    func taskCard(_ name: String) -> XCUIElement {
        app.groups[name].firstMatch
    }

    /// toggleTaskDetails expands the details fold of the named task card.
    func toggleTaskDetails(_ name: String, fold: String = "观测依据与输出") {
        let card = taskCard(name)
        expectExists(card, timeout: 5)
        let triangle = card.disclosureTriangles[fold]
        if triangle.exists {
            revealInContent(triangle)
            triangle.click()
            return
        }
        let button = card.buttons[fold]
        expectExists(button, timeout: 5)
        revealInContent(button)
        button.click()
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

    func prepare() {
        click("准备环境")
        wait("benchmark becomes available", timeout: 10) {
            self.app.buttons["开始测评"].exists && self.app.buttons["开始测评"].isEnabled
        }
    }

    /// chooseModel picks one entry from the task page's 测评模型 dropdown, the
    /// only place the pair is chosen. The dropdown lists installed models.
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
        let contentButton = app.scrollViews.element(boundBy: 1).buttons[label].firstMatch
        let button = contentButton.exists ? contentButton : app.buttons[label].firstMatch
        wait("button \(label) is enabled", timeout: timeout) { button.exists && button.isEnabled }
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
        waitForTab(name, timeout: timeout).click()
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
