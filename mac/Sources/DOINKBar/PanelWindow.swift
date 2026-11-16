import AppKit
import Combine
import SwiftUI

/// A borderless panel that can take keyboard input (the settings' text
/// fields) without activating the app, like Spotlight.
final class FloatingPanel: NSPanel {
    init() {
        super.init(contentRect: NSRect(x: 0, y: 0, width: PanelMetrics.width, height: 400),
                   styleMask: [.borderless, .nonactivatingPanel, .fullSizeContentView],
                   backing: .buffered, defer: true)
        isFloatingPanel = true
        level = .popUpMenu
        collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary, .transient]
        isOpaque = false
        backgroundColor = .clear
        hasShadow = true
        isMovable = false
        hidesOnDeactivate = false
        animationBehavior = .utilityWindow
    }

    override var canBecomeKey: Bool { true }
    override var canBecomeMain: Bool { false }
}

enum PanelMetrics {
    static let width: CGFloat = 340
    static let cornerRadius: CGFloat = 6
    static let gap: CGFloat = 5 // between the menu bar and the panel
    static let maxHeightFraction: CGFloat = 0.72 // of the screen's height
}

/// Owns the menu bar item and the panel: shows the panel under the item,
/// keeps its top edge pinned there while its height animates to fit the
/// content, and hides it on a click elsewhere or Esc.
@MainActor
final class PanelController: NSObject {
    private let statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
    private let panel = FloatingPanel()
    private let companion: Companion
    private let prefs: AppPrefs
    private var monitors: [Any] = []
    private var cancellables: Set<AnyCancellable> = []
    private var wantedHeight: CGFloat = 400

    init(companion: Companion, prefs: AppPrefs) {
        self.companion = companion
        self.prefs = prefs
        super.init()

        let root = PanelView(companion: companion, prefs: prefs, onHeight: { [weak self] h in self?.fit(h) },
                             onClose: { [weak self] in self?.hide() })
        let hosting = NSHostingView(rootView: root)
        hosting.sizingOptions = [] // the controller sizes the window, not SwiftUI
        panel.contentView = hosting

        if let button = statusItem.button {
            button.target = self
            button.action = #selector(toggle)
            button.sendAction(on: [.leftMouseUp, .rightMouseUp])
            button.setAccessibilityLabel("DOINK")
        }
        companion.objectWillChange
            .merge(with: prefs.objectWillChange)
            .receive(on: RunLoop.main)
            .sink { [weak self] in self?.refresh() }
            .store(in: &cancellables)
        refresh()
        // Development: DOINK_OPEN_PANEL=1 opens the panel at launch.
        if ProcessInfo.processInfo.environment["DOINK_OPEN_PANEL"] == "1" {
            DispatchQueue.main.asyncAfter(deadline: .now() + 1.5) { [weak self] in self?.show() }
        }
    }

    // MARK: Menu bar item

    private func refresh() {
        statusItem.button?.image = MenuBarIcon.image(for: companion, style: prefs.menuBarStyle)
        panel.appearance = prefs.theme.appearance
    }

    // MARK: Showing and hiding

    @objc private func toggle() {
        panel.isVisible ? hide() : show()
    }

    func show() {
        guard let frame = buttonScreenFrame() else { return }
        panel.setFrame(targetFrame(height: wantedHeight, under: frame), display: true)
        panel.makeKeyAndOrderFront(nil)
        statusItem.button?.highlight(true)
        companion.panelOpened()
        // A click in another app or on the desktop closes the panel.
        monitors.append(NSEvent.addGlobalMonitorForEvents(matching: [.leftMouseDown, .rightMouseDown]) { [weak self] _ in
            Task { @MainActor in self?.hide() }
        } as Any)
        monitors.append(NSEvent.addLocalMonitorForEvents(matching: .keyDown) { [weak self] event in
            if event.keyCode == 53, self?.panel.isKeyWindow == true { // Esc
                self?.hide()
                return nil
            }
            return event
        } as Any)
    }

    func hide() {
        guard panel.isVisible else { return }
        panel.orderOut(nil)
        statusItem.button?.highlight(false)
        monitors.forEach(NSEvent.removeMonitor)
        monitors = []
    }

    // MARK: Sizing

    /// The SwiftUI content measured itself: grow or shrink to it, top
    /// edge fixed under the menu bar.
    private func fit(_ height: CGFloat) {
        let height = ceil(height)
        guard abs(height - wantedHeight) > 0.5 else { return }
        wantedHeight = height
        guard panel.isVisible, let button = buttonScreenFrame() else { return }
        let target = targetFrame(height: height, under: button)
        NSAnimationContext.runAnimationGroup({ ctx in
            ctx.duration = prefs.reduceMotion ? 0 : 0.22
            ctx.timingFunction = CAMediaTimingFunction(name: .easeInEaseOut)
            panel.animator().setFrame(target, display: true)
        }, completionHandler: { [weak self] in
            Task { @MainActor in self?.panel.invalidateShadow() }
        })
    }

    private func buttonScreenFrame() -> NSRect? {
        guard let button = statusItem.button, let window = button.window else { return nil }
        return window.convertToScreen(button.convert(button.bounds, to: nil))
    }

    private func targetFrame(height: CGFloat, under button: NSRect) -> NSRect {
        let screen = NSScreen.screens.first { $0.frame.intersects(button) } ?? NSScreen.main
        let visible = screen?.visibleFrame ?? NSRect(x: 0, y: 0, width: 1440, height: 900)
        let top = min(button.minY, visible.maxY) - PanelMetrics.gap
        // At most a little under 3/4 of the screen; taller content scrolls.
        let cap = (screen?.frame.height ?? 900) * PanelMetrics.maxHeightFraction
        let h = min(height, cap, top - visible.minY - 8)
        var x = button.midX - PanelMetrics.width / 2
        x = max(visible.minX + 8, min(x, visible.maxX - PanelMetrics.width - 8))
        return NSRect(x: x, y: top - h, width: PanelMetrics.width, height: h)
    }
}
