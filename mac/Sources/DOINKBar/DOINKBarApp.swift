import AppKit
import SwiftUI

/// DOINK for macOS: a menu bar item that runs the companion and opens a
/// panel with your last character, what was posted, and the settings.
///
/// The item and panel are AppKit (PanelController), not SwiftUI's
/// MenuBarExtra: MenuBarExtra resizes its window from the bottom edge, so
/// a shorter tab left the panel hanging below the menu bar. Our own panel
/// keeps its top edge fixed and animates its height.
@main
struct DOINKBarApp: App {
    @NSApplicationDelegateAdaptor(AppDelegate.self) private var delegate

    var body: some Scene {
        Settings { EmptyView() } // everything lives in the panel
    }
}

@MainActor
final class AppDelegate: NSObject, NSApplicationDelegate {
    private var companion: Companion?
    private var prefs: AppPrefs?
    private var panel: PanelController?

    func applicationWillFinishLaunching(_ notification: Notification) {
        // A second copy would run a second companion and post every
        // event twice: hand over to the first one.
        let me = NSRunningApplication.current
        if let id = Bundle.main.bundleIdentifier,
           NSRunningApplication.runningApplications(withBundleIdentifier: id).contains(where: { $0 != me }) {
            let alert = NSAlert()
            alert.messageText = "DOINK is already running"
            alert.informativeText = "Look for the D in your menu bar."
            alert.runModal()
            exit(0)
        }
    }

    func applicationDidFinishLaunching(_ notification: Notification) {
        let companion = Companion()
        let prefs = AppPrefs()
        self.companion = companion
        self.prefs = prefs
        panel = PanelController(companion: companion, prefs: prefs)
    }
}
