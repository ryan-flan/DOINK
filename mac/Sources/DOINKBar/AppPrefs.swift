import AppKit
import Foundation

/// The app's own preferences (how it looks), in UserDefaults. The
/// companion's settings live in config.toml instead: see SettingsStore.
@MainActor
final class AppPrefs: ObservableObject {
    enum Theme: String, CaseIterable, Identifiable {
        case system = "System", light = "Light", dark = "Dark"
        var id: String { rawValue }
        var appearance: NSAppearance? {
            switch self {
            case .system: return nil
            case .light: return NSAppearance(named: .aqua)
            case .dark: return NSAppearance(named: .darkAqua)
            }
        }
    }

    enum MenuBarStyle: String, CaseIterable, Identifiable {
        case nameAndLevel = "Name and level"
        case level = "Level only"
        case icon = "Icon only"
        var id: String { rawValue }
    }

    private let defaults = UserDefaults.standard

    @Published var theme: Theme {
        didSet { defaults.set(theme.rawValue, forKey: "theme") }
    }
    @Published var menuBarStyle: MenuBarStyle {
        didSet { defaults.set(menuBarStyle.rawValue, forKey: "menuBarStyle") }
    }
    @Published var reduceMotion: Bool {
        didSet { defaults.set(reduceMotion, forKey: "reduceMotion") }
    }

    init() {
        theme = Theme(rawValue: defaults.string(forKey: "theme") ?? "") ?? .system
        menuBarStyle = MenuBarStyle(rawValue: defaults.string(forKey: "menuBarStyle") ?? "") ?? .nameAndLevel
        reduceMotion = defaults.bool(forKey: "reduceMotion")
    }
}
