import AppKit
import XCTest

@MainActor
func positionPreviewWindowForCapture(_ window: XCUIElement) {
    let screen = NSScreen.screens[0]
    let visible = screen.visibleFrame
    let desktop = CGRect(x: visible.minX, y: screen.frame.maxY - visible.maxY, width: visible.width, height: visible.height)
    let size = CGSize(width: min(1024, desktop.width), height: min(681, desktop.height))
    let corner = window.coordinate(withNormalizedOffset: CGVector(dx: 1, dy: 1)).withOffset(CGVector(dx: -2, dy: -2))
    if window.frame.width > size.width || window.frame.height > size.height {
        corner.press(forDuration: 0.2, thenDragTo: corner.withOffset(CGVector(dx: size.width - window.frame.width, dy: size.height - window.frame.height)))
    }
    let origin = window.frame.origin
    let titlebar = window.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.02))
    let x = desktop.minX + max(0, (desktop.width - window.frame.width) / 2)
    let y = desktop.minY + max(0, (desktop.height - window.frame.height) / 2)
    titlebar.press(forDuration: 0.2, thenDragTo: titlebar.withOffset(CGVector(dx: x - origin.x, dy: y - origin.y)))
    XCTAssertTrue(desktop.insetBy(dx: -1, dy: -1).contains(window.frame), "The test window must fit above the Dock and inside the display")
}
