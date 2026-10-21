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
        XCTAssertTrue(app.windows["InferYard Studio · UI 调试"].frame.contains(list.frame), "Scene choices must stay inside the window")
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
        XCTAssertTrue(content.frame.contains(element.frame), "The complete control must be visible after scrolling")
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
            self.app.buttons["运行 benchmark"].exists && self.app.buttons["运行 benchmark"].isEnabled
        }
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

    func expectText(_ text: String, timeout: TimeInterval = 10) {
        let element = self.text(text)
        XCTAssertTrue(element.exists || element.waitForExistence(timeout: timeout), "Missing text: \(text)")
    }

    func expectExists(_ element: XCUIElement, timeout: TimeInterval) {
        XCTAssertTrue(element.exists || element.waitForExistence(timeout: timeout))
    }

    func text(_ value: String) -> XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "label == %@ OR value == %@", value, value)).firstMatch
    }

    func wait(_ description: String, timeout: TimeInterval, condition: @escaping () -> Bool) {
        if condition() { return }
        let expectation = XCTNSPredicateExpectation(predicate: NSPredicate { _, _ in condition() }, object: nil)
        XCTAssertEqual(XCTWaiter.wait(for: [expectation], timeout: timeout), .completed, description)
    }
}
