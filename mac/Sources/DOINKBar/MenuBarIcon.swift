import AppKit

/// The menu bar item: DOINK's "D" and, like OpenUsage's "Session 18%",
/// two small lines of text (the last character's name and level). Drawn as
/// a template image so macOS tints it for light and dark menu bars.
enum MenuBarIcon {
    /// The item for the companion's state, in the chosen style.
    @MainActor
    static func image(for companion: Companion, style: AppPrefs.MenuBarStyle) -> NSImage {
        let plain = image(top: nil, bottom: nil)
        switch companion.phase {
        case .stopped:
            return style == .icon ? plain : image(top: nil, bottom: "off")
        case .starting:
            return plain
        case .running:
            guard let s = companion.status else { return plain }
            if !s.error.isEmpty { return style == .icon ? plain : image(top: nil, bottom: "!") }
            guard let toon = s.toons.last, style != .icon else { return plain }
            let level = toon.level.map { "Lv \($0)" } ?? "–"
            return style == .level ? image(top: nil, bottom: level) : image(top: toon.name, bottom: level)
        }
    }

    static func image(top: String?, bottom: String?) -> NSImage {
        let height: CGFloat = 18
        let glyph: CGFloat = 16
        let topAttrs: [NSAttributedString.Key: Any] = [.font: NSFont.systemFont(ofSize: 8.5, weight: .medium)]
        let bottomAttrs: [NSAttributedString.Key: Any] = [.font: NSFont.monospacedDigitSystemFont(ofSize: 10, weight: .semibold)]
        let topText = top.map { NSAttributedString(string: $0, attributes: topAttrs) }
        let bottomText = bottom.map { NSAttributedString(string: $0, attributes: bottomAttrs) }
        let textWidth = ceil(max(topText?.size().width ?? 0, bottomText?.size().width ?? 0))
        let width = glyph + (textWidth > 0 ? 3 + textWidth : 0)

        let image = NSImage(size: NSSize(width: width, height: height), flipped: false) { _ in
            if let cg = NSGraphicsContext.current?.cgContext {
                cg.addPath(dPath(in: NSRect(x: 0, y: (height - glyph) / 2, width: glyph, height: glyph)).cgPath)
                cg.setFillColor(NSColor.black.cgColor)
                cg.fillPath(using: .evenOdd) // cut out the bowl
            }
            let x = glyph + 3
            switch (topText, bottomText) {
            case let (t?, b?):
                t.draw(at: NSPoint(x: x, y: 8.5))
                b.draw(at: NSPoint(x: x, y: -1.5))
            case let (t?, nil):
                t.draw(at: NSPoint(x: x, y: (height - t.size().height) / 2))
            case let (nil, b?):
                b.draw(at: NSPoint(x: x, y: (height - b.size().height) / 2))
            default:
                break
            }
            return true
        }
        image.isTemplate = true
        return image
    }

    /// The logo's D: a bar with a rounded bowl, hollow inside (even-odd).
    static func dPath(in r: NSRect) -> NSBezierPath {
        func p(_ x: CGFloat, _ y: CGFloat) -> NSPoint {
            NSPoint(x: r.minX + x / 16 * r.width, y: r.minY + y / 16 * r.height)
        }
        let path = NSBezierPath()
        path.windingRule = .evenOdd
        // Outer edge.
        path.move(to: p(3, 15))
        path.line(to: p(3, 1))
        path.curve(to: p(14.5, 8), controlPoint1: p(9.5, 1), controlPoint2: p(14.5, 3.5))
        path.curve(to: p(3, 15), controlPoint1: p(14.5, 12.5), controlPoint2: p(9.5, 15))
        path.close()
        // Inner hole.
        path.move(to: p(6, 12.2))
        path.line(to: p(6, 3.8))
        path.curve(to: p(11.4, 8), controlPoint1: p(9, 4.1), controlPoint2: p(11.4, 5.6))
        path.curve(to: p(6, 12.2), controlPoint1: p(11.4, 10.4), controlPoint2: p(9, 11.9))
        path.close()
        return path
    }
}
