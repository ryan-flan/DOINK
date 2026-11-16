import AppKit
import ServiceManagement
import SwiftUI

/// The panel that opens from the menu bar, laid out like OpenUsage's:
/// a segmented control, ring rows grouped under small headings, and a
/// footer pinned to the bottom. The gear opens the settings page. The
/// view measures itself and reports its height (onHeight), so the panel
/// can grow or shrink to fit with its top edge under the menu bar.
struct PanelView: View {
    @ObservedObject var companion: Companion
    @ObservedObject var prefs: AppPrefs
    var onHeight: (CGFloat) -> Void
    var onClose: () -> Void

    @StateObject private var store: SettingsStore
    @State private var tab: Tab = .toon
    @State private var page: Page = .main
    @State private var contentHeight: CGFloat = 0
    @State private var footerHeight: CGFloat = 0

    enum Tab: String, CaseIterable {
        case toon = "Character"
        case activity = "Activity"
    }

    enum Page { case main, settings }

    init(companion: Companion, prefs: AppPrefs, onHeight: @escaping (CGFloat) -> Void, onClose: @escaping () -> Void) {
        self.companion = companion
        self.prefs = prefs
        self.onHeight = onHeight
        self.onClose = onClose
        _store = StateObject(wrappedValue: SettingsStore(companion: companion))
    }

    var body: some View {
        VStack(spacing: 0) {
            ScrollView(.vertical) { // scrolls once the panel hits its max height
                Group {
                    switch page {
                    case .main: main
                    case .settings:
                        SettingsView(companion: companion, prefs: prefs, store: store) { go(.main) }
                    }
                }
                .padding(16)
                // The scroll view's width, not a fixed 340: a legacy
                // scroller ("Show scroll bars: Always") would otherwise
                // push fixed-width content sideways.
                .frame(maxWidth: .infinity, alignment: .topLeading)
                .background(Measure(key: ContentHeight.self))
            }
            .scrollBounceBehavior(.basedOnSize)
            // While the window animates to a taller tab, the content is
            // briefly taller than the window; a scroller flashing in and
            // out then shook the layout. Only show one when the content
            // really is taller than the panel can get.
            .scrollIndicators(needsScroll ? .automatic : .never)
            Footer(companion: companion, onSettings: page == .main ? { go(.settings) } : nil)
                .padding(.horizontal, 16).padding(.vertical, 12)
                .background(Palette.bar)
                .background(Measure(key: FooterHeight.self))
                .overlay(alignment: .top) { RowDivider() }
        }
        .frame(width: PanelMetrics.width)
        .frame(maxHeight: .infinity, alignment: .top)
        .background(Palette.background)
        .clipShape(RoundedRectangle(cornerRadius: PanelMetrics.cornerRadius, style: .continuous))
        .overlay(RoundedRectangle(cornerRadius: PanelMetrics.cornerRadius, style: .continuous)
            .strokeBorder(Palette.border, lineWidth: 0.5))
        .onPreferenceChange(ContentHeight.self) { contentHeight = $0; report() }
        .onPreferenceChange(FooterHeight.self) { footerHeight = $0; report() }
    }

    private var main: some View {
        VStack(alignment: .leading, spacing: 14) {
            header
            Segmented(selection: $tab, reduceMotion: prefs.reduceMotion)
            Group {
                switch tab {
                case .toon: ToonTab(status: companion.status, now: companion.now)
                case .activity: ActivityTab(companion: companion)
                }
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }

    private var header: some View {
        HStack(spacing: 10) {
            LogoMark().frame(width: 26, height: 26)
            Text("DOINK").font(.system(size: 15, weight: .bold)).foregroundStyle(Palette.primary)
            Spacer()
            StatusPill(companion: companion)
        }
    }

    private var needsScroll: Bool {
        let screen = NSScreen.main?.frame.height ?? 900
        return contentHeight + footerHeight > screen * PanelMetrics.maxHeightFraction
    }

    private func go(_ p: Page) {
        if prefs.reduceMotion { page = p } else { withAnimation(.easeInOut(duration: 0.18)) { page = p } }
    }

    private func report() {
        guard contentHeight > 0 else { return }
        onHeight(contentHeight + footerHeight)
    }
}

private struct ContentHeight: PreferenceKey {
    static var defaultValue: CGFloat = 0
    static func reduce(value: inout CGFloat, nextValue: () -> CGFloat) { value = max(value, nextValue()) }
}

private struct FooterHeight: PreferenceKey {
    static var defaultValue: CGFloat = 0
    static func reduce(value: inout CGFloat, nextValue: () -> CGFloat) { value = max(value, nextValue()) }
}

/// Reports the size of the view it is the background of.
private struct Measure<Key: PreferenceKey>: View where Key.Value == CGFloat {
    var key: Key.Type
    var body: some View {
        GeometryReader { Color.clear.preference(key: Key.self, value: $0.size.height) }
    }
}

// MARK: - Pieces

/// The app logo: gold D on charcoal.
struct LogoMark: View {
    var body: some View {
        GeometryReader { geo in
            ZStack {
                RoundedRectangle(cornerRadius: geo.size.width * 0.24, style: .continuous).fill(Palette.charcoal)
                DShape().fill(Palette.gold, style: FillStyle(eoFill: true))
                    .padding(geo.size.width * 0.18)
            }
        }
    }
}

struct DShape: Shape {
    func path(in rect: CGRect) -> Path {
        // MenuBarIcon's path is y-up; flip it for SwiftUI.
        var path = Path(MenuBarIcon.dPath(in: rect).cgPath)
        path = path.applying(CGAffineTransform(a: 1, b: 0, c: 0, d: -1, tx: 0, ty: rect.minY * 2 + rect.height))
        return path
    }
}

struct StatusPill: View {
    @ObservedObject var companion: Companion

    var body: some View {
        let (text, color) = state
        HStack(spacing: 5) {
            Circle().fill(color).frame(width: 7, height: 7)
            Text(text).font(.system(size: 11, weight: .semibold)).foregroundStyle(Palette.secondary)
        }
        .padding(.horizontal, 9).padding(.vertical, 4)
        .background(Capsule().fill(Palette.track))
    }

    private var state: (String, Color) {
        switch companion.phase {
        case .starting: return ("Starting", Palette.tertiary)
        case .stopped: return ("Stopped", Palette.red)
        case .running:
            guard let s = companion.status else { return ("Starting", Palette.tertiary) }
            if !s.error.isEmpty { return ("Needs attention", Palette.orange) }
            if s.dryRun { return ("Dry run", Palette.blue) }
            return ("Running", Palette.green)
        }
    }
}

/// OpenUsage's capsule segmented control.
struct Segmented: View {
    @Binding var selection: PanelView.Tab
    var reduceMotion = false
    @Namespace private var ns

    var body: some View {
        HStack(spacing: 0) {
            ForEach(PanelView.Tab.allCases, id: \.self) { tab in
                let on = tab == selection
                Text(tab.rawValue)
                    .font(.system(size: 13, weight: on ? .semibold : .regular))
                    .foregroundStyle(on ? Palette.primary : Palette.secondary)
                    .frame(maxWidth: .infinity).padding(.vertical, 6)
                    .background {
                        if on {
                            Capsule().fill(Palette.selected)
                                .shadow(color: .black.opacity(0.12), radius: 2, y: 1)
                                .matchedGeometryEffect(id: "seg", in: ns)
                        }
                    }
                    .contentShape(Capsule())
                    .onTapGesture {
                        if reduceMotion { selection = tab } else {
                            withAnimation(.spring(response: 0.25, dampingFraction: 0.9)) { selection = tab }
                        }
                    }
            }
        }
        .padding(3)
        .background(Capsule().fill(Palette.track))
    }
}

/// A progress ring with something in the middle (OpenUsage's row icon).
struct Ring<Content: View>: View {
    var progress: Double
    var color: Color
    var size: CGFloat = 44
    var line: CGFloat = 4
    @ViewBuilder var content: Content

    var body: some View {
        ZStack {
            Circle().stroke(Palette.ringTrack, lineWidth: line)
            // Unknown (0) draws no arc; anything known shows at least a dot.
            Circle().trim(from: 0, to: progress > 0 ? max(0.02, min(1, progress)) : 0)
                .stroke(color, style: StrokeStyle(lineWidth: line, lineCap: .round))
                .rotationEffect(.degrees(-90))
            content
        }
        .frame(width: size, height: size)
    }
}

struct SectionTitle: View {
    var text: String
    var body: some View {
        Text(text).font(.system(size: 12, weight: .semibold)).foregroundStyle(Palette.secondary)
    }
}

struct Card<Content: View>: View {
    @ViewBuilder var content: Content
    var body: some View {
        VStack(alignment: .leading, spacing: 10) { content }
            .padding(12)
            .frame(maxWidth: .infinity, alignment: .leading)
            .background(RoundedRectangle(cornerRadius: 12, style: .continuous).fill(Palette.card))
    }
}

// MARK: - Character tab

/// Forever's level cap, for the level ring.
private let maxLevel = 60.0

struct ToonTab: View {
    var status: CompanionStatus?
    var now: Date

    var body: some View {
        if let toon = status?.toons.last {
            VStack(alignment: .leading, spacing: 14) {
                SectionTitle(text: "Last logged in")
                Hero(toon: toon, now: now)
                if !toon.recent.isEmpty {
                    SectionTitle(text: "Recent")
                    VStack(spacing: 8) {
                        ForEach(toon.recent.prefix(5), id: \.self) { e in EventRow(event: e, now: now) }
                    }
                }
                if let others = status?.toons.others, !others.isEmpty {
                    SectionTitle(text: "Other characters")
                    VStack(spacing: 10) {
                        ForEach(others.prefix(4)) { t in ToonRow(toon: t, now: now) }
                    }
                }
            }
        } else {
            Empty(symbol: "person.crop.circle.badge.questionmark",
                  title: status == nil ? "Waiting for the companion" : "No characters yet",
                  text: "Log in with the DOINK addon enabled. Your characters show up here after a /reload or logout.")
        }
    }
}

struct Hero: View {
    var toon: Toon
    var now: Date

    var body: some View {
        let color = Palette.classColor(toon.class)
        Card {
            HStack(spacing: 12) {
                Ring(progress: Double(toon.level ?? 0) / maxLevel, color: color, size: 58, line: 5) {
                    VStack(spacing: -2) {
                        Text(toon.level.map(String.init) ?? "?")
                            .font(.system(size: 19, weight: .bold, design: .rounded)).foregroundStyle(Palette.primary)
                        Text("LVL").font(.system(size: 8, weight: .semibold)).foregroundStyle(Palette.secondary)
                    }
                }
                VStack(alignment: .leading, spacing: 2) {
                    Text(toon.fullName).font(.system(size: 17, weight: .semibold)).foregroundStyle(Palette.primary)
                    Text(toon.className ?? toon.subtitle).font(.system(size: 13, weight: .medium))
                        .foregroundStyle(toon.className == nil ? Palette.secondary : color)
                    if let realm = toon.realm, !realm.isEmpty {
                        Text(realm).font(.system(size: 11)).foregroundStyle(Palette.secondary).lineLimit(1)
                    }
                }
                Spacer(minLength: 0)
                if let seen = toon.lastSeen {
                    VStack(alignment: .trailing, spacing: 2) {
                        Text(Format.ago(seen, now: now)).font(.system(size: 15, weight: .semibold)).foregroundStyle(Palette.primary)
                        Text("last seen").font(.system(size: 11)).foregroundStyle(Palette.secondary)
                    }
                }
            }
            HStack(spacing: 8) {
                Stat(value: toon.counts["level_up"] ?? 0, label: "Levels", color: Palette.gold)
                Stat(value: toon.counts["boss_kill"] ?? 0, label: "Bosses", color: Palette.orange)
                Stat(value: toon.counts["quest"] ?? 0, label: "Quests", color: Palette.blue)
                Stat(value: toon.counts["death"] ?? 0, label: "Deaths", color: Palette.red)
            }
            if let played = toon.played, played > 0 {
                HStack {
                    Image(systemName: "clock").foregroundStyle(Palette.secondary)
                    Text("Time played").foregroundStyle(Palette.secondary)
                    Spacer()
                    Text(Format.played(played)).foregroundStyle(Palette.primary).monospacedDigit()
                }
                .font(.system(size: 12))
            }
        }
    }
}

struct Stat: View {
    var value: Int
    var label: String
    var color: Color
    var body: some View {
        VStack(spacing: 2) {
            Text("\(value)").font(.system(size: 15, weight: .semibold, design: .rounded)).foregroundStyle(Palette.primary)
            Text(label).font(.system(size: 10, weight: .medium)).foregroundStyle(Palette.secondary)
        }
        .frame(maxWidth: .infinity).padding(.vertical, 7)
        .background(RoundedRectangle(cornerRadius: 8, style: .continuous).fill(color.opacity(0.13)))
    }
}

struct EventRow: View {
    var event: ToonEvent
    var now: Date
    var body: some View {
        HStack(spacing: 10) {
            Image(systemName: event.symbol).font(.system(size: 14)).foregroundStyle(Palette.eventColor(event.type))
                .frame(width: 20)
            Text(event.text).font(.system(size: 13)).foregroundStyle(Palette.primary).lineLimit(1)
            Spacer(minLength: 6)
            Text(Format.ago(event.ts, now: now)).font(.system(size: 12)).foregroundStyle(Palette.secondary).monospacedDigit()
        }
    }
}

struct ToonRow: View {
    var toon: Toon
    var now: Date
    var body: some View {
        HStack(spacing: 12) {
            Ring(progress: Double(toon.level ?? 0) / maxLevel, color: Palette.classColor(toon.class), size: 34, line: 3) {
                Text(toon.level.map(String.init) ?? "–")
                    .font(.system(size: 11, weight: .bold, design: .rounded)).foregroundStyle(Palette.primary)
            }
            VStack(alignment: .leading, spacing: 1) {
                Text(toon.fullName).font(.system(size: 14, weight: .medium)).foregroundStyle(Palette.primary)
                Text(toon.subtitle).font(.system(size: 12)).foregroundStyle(Palette.secondary)
            }
            Spacer()
            if let seen = toon.lastSeen {
                Text(Format.ago(seen, now: now)).font(.system(size: 12)).foregroundStyle(Palette.secondary)
            }
        }
    }
}

struct Empty: View {
    var symbol: String
    var title: String
    var text: String
    var body: some View {
        VStack(spacing: 8) {
            Image(systemName: symbol).font(.system(size: 28)).foregroundStyle(Palette.tertiary)
            Text(title).font(.system(size: 14, weight: .semibold)).foregroundStyle(Palette.primary)
            Text(text).font(.system(size: 12)).foregroundStyle(Palette.secondary).multilineTextAlignment(.center)
        }
        .frame(maxWidth: .infinity).padding(.vertical, 24)
    }
}

// MARK: - Activity tab

struct ActivityTab: View {
    @ObservedObject var companion: Companion

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            if case let .stopped(why) = companion.phase {
                Banner(text: why, color: Palette.red)
            } else if let err = companion.status?.error, !err.isEmpty {
                Banner(text: err, color: Palette.orange)
            }
            SectionTitle(text: "Posted to Discord")
            if let recent = companion.status?.recent, !recent.isEmpty {
                VStack(spacing: 8) {
                    ForEach(recent, id: \.self) { p in
                        HStack(spacing: 10) {
                            Image(systemName: "paperplane.fill").font(.system(size: 12)).foregroundStyle(Palette.blue).frame(width: 20)
                            VStack(alignment: .leading, spacing: 0) {
                                Text(Format.type(p.what)).font(.system(size: 13)).foregroundStyle(Palette.primary)
                                Text(p.who).font(.system(size: 11)).foregroundStyle(Palette.secondary)
                            }
                            Spacer()
                            Text(Format.ago(p.at, now: companion.now)).font(.system(size: 12)).foregroundStyle(Palette.secondary)
                        }
                    }
                }
            } else {
                Text("Nothing posted since the companion started.").font(.system(size: 12)).foregroundStyle(Palette.secondary)
            }
            SectionTitle(text: "Watching")
            if let files = companion.status?.files, !files.isEmpty {
                ForEach(files, id: \.self) { f in
                    Text(shortPath(f)).font(.system(size: 11, design: .monospaced)).foregroundStyle(Palette.secondary)
                        .lineLimit(1).truncationMode(.middle)
                        .help(f)
                }
            } else {
                Text("No DOINK.lua found yet.").font(.system(size: 12)).foregroundStyle(Palette.secondary)
            }
        }
    }

    private func shortPath(_ path: String) -> String {
        // ".../_forever_/WTF/Account/<acct>/SavedVariables/DOINK.lua" -> "_forever_ · <acct>"
        let parts = path.split(separator: "/")
        if let i = parts.firstIndex(of: "Account"), i + 1 < parts.count, i >= 2 {
            return "\(parts[i - 2]) · \(parts[i + 1])"
        }
        return path
    }
}

struct Banner: View {
    var text: String
    var color: Color
    var symbol = "exclamationmark.triangle.fill"
    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            Image(systemName: symbol).foregroundStyle(color)
            Text(text).font(.system(size: 12)).foregroundStyle(Palette.primary).fixedSize(horizontal: false, vertical: true)
        }
        .padding(10)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(RoundedRectangle(cornerRadius: 10, style: .continuous).fill(color.opacity(0.14)))
    }
}

// MARK: - Footer

struct Footer: View {
    @ObservedObject var companion: Companion
    var onSettings: (() -> Void)?

    var body: some View {
        HStack {
            VStack(alignment: .leading, spacing: 1) {
                Text(versionLine).font(.system(size: 11)).foregroundStyle(Palette.secondary)
                Text(refreshLine).font(.system(size: 11)).foregroundStyle(Palette.secondary).monospacedDigit()
            }
            Spacer()
            if let onSettings {
                CircleButton(symbol: "doc.on.doc", help: "Copy a summary") { copySummary() }
                CircleButton(symbol: "gearshape", help: "Settings", action: onSettings)
            }
        }
    }

    private var versionLine: String {
        guard let v = companion.status?.version else { return "DOINK" }
        return v == "dev" ? "DOINK (dev build)" : "DOINK v\(v)"
    }

    private var refreshLine: String {
        guard let s = companion.status else { return "Not running" }
        let next = max(0, Int(s.updated) + s.every - Int(companion.now.timeIntervalSince1970))
        return "Next update in \(next)s"
    }

    private func copySummary() {
        guard let t = companion.status?.toons.last else { return }
        var lines = ["\(t.fullName): \(t.subtitle)" + (t.realm.map { " on \($0)" } ?? "")]
        lines += t.recent.prefix(5).map { "• \($0.text)" }
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(lines.joined(separator: "\n"), forType: .string)
    }
}

struct CircleButton: View {
    var symbol: String
    var help: String
    var action: () -> Void
    var body: some View {
        Button(action: action) {
            Image(systemName: symbol).font(.system(size: 13, weight: .medium)).foregroundStyle(Palette.primary)
                .frame(width: 32, height: 32).background(Circle().fill(Palette.track))
        }
        .buttonStyle(.plain).help(help)
    }
}
