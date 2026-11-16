import AppKit
import SwiftUI

/// Colours after OpenUsage's menu bar panel (neutral cards, blue/orange/red
/// rings, a capsule segmented control), plus DOINK's gold and the WoW
/// class colours. Every colour has a light and a dark value; the dark
/// values are sampled from OpenUsage 0.7.9's dark settings page.
enum Palette {
    static let background = dynamic(light: 0xF5F5F7, dark: 0x282828)
    static let card = dynamic(light: 0xFFFFFF, dark: 0x303030)
    static let bar = dynamic(light: 0xF5F5F7, dark: 0x242424)        // footer
    static let track = dynamic(light: 0xE5E5EA, dark: 0x444444)      // pills, segmented track, round buttons
    static let selected = dynamic(light: 0xFFFFFF, dark: 0x5A5A5A)   // selected segment
    static let ringTrack = dynamic(light: 0xE5E5EA, dark: 0x3C3C3C)
    static let primary = dynamic(light: 0x1D1D1F, dark: 0xF6F6F6)
    static let secondary = dynamic(light: 0x6E6E73, dark: 0xABABAB)
    static let tertiary = dynamic(light: 0xAEAEB2, dark: 0x6E6E6E)
    static let divider = dynamic(light: 0xE5E5EA, dark: 0x3A3A3A)
    static let border = dynamic(light: 0xD1D1D6, dark: 0x5D5D5D)     // the panel's outline

    static let blue = dynamic(light: 0x3B82F6, dark: 0x4792F7)
    static let orange = Color(hex: 0xF59E0B)
    static let red = Color(hex: 0xEF4444)
    static let green = Color(hex: 0x34C77B)
    static let gold = Color(hex: 0xF2B83A)        // the D in DOINK's logo
    static let charcoal = Color(hex: 0x202326)    // the logo's background

    /// WoW's class colours. Priest is white, so in light mode it is grey.
    static func classColor(_ token: String?) -> Color {
        switch token {
        case "WARRIOR": return Color(hex: 0xC69B6D)
        case "PALADIN": return Color(hex: 0xF48CBA)
        case "HUNTER": return Color(hex: 0xAAD372)
        case "ROGUE": return dynamic(light: 0xC9B800, dark: 0xFFF468)
        case "PRIEST": return dynamic(light: 0x8E8E93, dark: 0xFFFFFF)
        case "SHAMAN": return Color(hex: 0x0070DD)
        case "MAGE": return Color(hex: 0x3FC7EB)
        case "WARLOCK": return Color(hex: 0x8788EE)
        case "DRUID": return Color(hex: 0xFF7C0A)
        case "DEATHKNIGHT": return Color(hex: 0xC41E3A)
        default: return dynamic(light: 0x3B82F6, dark: 0x4792F7)
        }
    }

    static func eventColor(_ type: String) -> Color {
        switch type {
        case "level_up": return gold
        case "death": return red
        case "boss_kill": return orange
        case "loot": return Color(hex: 0xA335EE)
        case "quest": return blue
        default: return green
        }
    }

    private static func dynamic(light: UInt32, dark: UInt32) -> Color {
        Color(nsColor: NSColor(name: nil) { appearance in
            let isDark = appearance.bestMatch(from: [.darkAqua, .aqua]) == .darkAqua
            return NSColor(hex: isDark ? dark : light)
        })
    }
}

extension NSColor {
    convenience init(hex: UInt32) {
        self.init(srgbRed: CGFloat(hex >> 16 & 0xFF) / 255,
                  green: CGFloat(hex >> 8 & 0xFF) / 255,
                  blue: CGFloat(hex & 0xFF) / 255, alpha: 1)
    }
}

extension Color {
    init(hex: UInt32) { self.init(nsColor: NSColor(hex: hex)) }
}
