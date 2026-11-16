import AppKit
import ServiceManagement
import SwiftUI

/// The settings page inside the panel, laid out like OpenUsage's: a back
/// button, then grouped cards of rows. Companion settings are saved to
/// config.toml as soon as they change; the app's own (theme, menu bar)
/// to UserDefaults.
struct SettingsView: View {
    @ObservedObject var companion: Companion
    @ObservedObject var prefs: AppPrefs
    @ObservedObject var store: SettingsStore
    var onBack: () -> Void

    @State private var launchAtLogin = SMAppService.mainApp.status == .enabled
    @State var advanced = false

    var body: some View {
        VStack(alignment: .leading, spacing: 14) {
            header
            if let m = store.message {
                Banner(text: m.text, color: m.isError ? Palette.red : Palette.blue,
                       symbol: m.isError ? "exclamationmark.triangle.fill" : "info.circle.fill")
            }
            general
            discord
            game
            companionSection
            files
        }
        .task { await store.load() }
    }

    private var header: some View {
        ZStack {
            Text("Settings").font(.system(size: 15, weight: .bold)).foregroundStyle(Palette.primary)
            HStack {
                CircleButton(symbol: "chevron.left", help: "Back", action: onBack)
                Spacer()
                if store.loading { ProgressView().controlSize(.small) }
            }
        }
    }

    // MARK: Sections

    private var general: some View {
        Group {
            SectionTitle(text: "General")
            Card {
                ToggleRow(title: "Launch at Login", isOn: Binding(get: { launchAtLogin }, set: setLaunchAtLogin))
                RowDivider()
                PickerRow(title: "Menu Bar", selection: $prefs.menuBarStyle, options: AppPrefs.MenuBarStyle.allCases)
                RowDivider()
                PickerRow(title: "Theme", selection: $prefs.theme, options: AppPrefs.Theme.allCases)
                RowDivider()
                ToggleRow(title: "Reduce Animations", isOn: $prefs.reduceMotion)
            }
        }
    }

    private var discord: some View {
        Group {
            SectionTitle(text: "Discord")
            Card {
                URLRow(title: "Webhook URL", placeholder: "https://discord.com/api/webhooks/…",
                       value: store.string("webhook_url"),
                       save: { await store.set("webhook_url", $0) },
                       test: { await store.test($0) })
                Caption(text: webhookCaption)
                RowDivider()
                URLRow(title: "Forward URL", placeholder: "https://… (your own tool)",
                       value: store.string("forward_url"),
                       save: { await store.set("forward_url", $0) }, test: nil)
                Caption(text: "Also POST each event's JSON here, for your own bot or spreadsheet. Leave empty for Discord only.")
                RowDivider()
                advancedToggle
                if advanced {
                    Caption(text: "Send an action to its own channel. These win over every other webhook, including ones set in game; empty ones use the webhooks above.")
                    ForEach(SettingsStore.actions, id: \.type) { action in
                        RowDivider()
                        URLRow(title: action.label, placeholder: "https://discord.com/api/webhooks/…",
                               value: store.route(action.type),
                               save: { await store.set("webhooks.\(action.type)", $0) },
                               test: { draft in await store.test(draft.isEmpty ? store.route(action.type) : draft) })
                    }
                }
            }
        }
    }

    private var advancedToggle: some View {
        Button {
            if prefs.reduceMotion { advanced.toggle() } else { withAnimation(.easeInOut(duration: 0.2)) { advanced.toggle() } }
        } label: {
            HStack {
                Text("Advanced: Webhook per Action").rowTitle()
                Spacer()
                let n = store.view?.routes.count ?? 0
                if n > 0 { Text("\(n) set").font(.system(size: 12)).foregroundStyle(Palette.secondary) }
                Image(systemName: "chevron.right").font(.system(size: 11, weight: .semibold)).foregroundStyle(Palette.tertiary)
                    .rotationEffect(.degrees(advanced ? 90 : 0))
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    private var webhookCaption: String {
        let game = store.view?.gameWebhooks ?? ""
        let base = "Used for characters without a webhook set in game. In game: /doink webhook <url> (per character, or all)."
        return game.isEmpty ? base : "Set in game for \(game), which wins over this one. " + base
    }

    private var game: some View {
        Group {
            SectionTitle(text: "World of Warcraft")
            Card {
                HStack(alignment: .firstTextBaseline) {
                    Text("Game Folder").rowTitle()
                    Spacer()
                    if !store.string("wow_dir").isEmpty {
                        Button("Auto") { Task { await store.set("wow_dir", "") } }.buttonStyle(PillButton())
                    }
                    Button("Choose…", action: chooseFolder).buttonStyle(PillButton())
                }
                Text(folderLine).font(.system(size: 11, design: .monospaced)).foregroundStyle(Palette.secondary)
                    .lineLimit(2).truncationMode(.middle)
                RowDivider()
                ToggleRow(title: "Instant Posts", isOn: boolBinding("realtime", default: true))
                Caption(text: "Read the screenshots DOINK takes for realtime posts. Also turn it on in game: /doink realtime on.")
            }
        }
    }

    private var folderLine: String {
        let set = store.string("wow_dir")
        if !set.isEmpty { return set }
        if let found = store.view?.wowDirs.first { return found + " (found automatically)" }
        return store.view == nil ? "…" : "Not found. Choose the folder that contains _forever_."
    }

    private var companionSection: some View {
        Group {
            SectionTitle(text: "Companion")
            Card {
                ToggleRow(title: "Item Details from Wowhead", isOn: boolBinding("wowhead", default: true))
                RowDivider()
                ToggleRow(title: "Dry Run (don't post)", isOn: boolBinding("dry_run", default: false))
                RowDivider()
                StepperRow(title: "Check Every", value: store.number("poll_interval", default: 2), range: 1...10, step: 1,
                           format: { "\(Int($0)) s" }) { v in Task { await store.set("poll_interval", String(Int(v))) } }
                RowDivider()
                StepperRow(title: "New Character Backlog", value: store.number("max_backlog", default: 10), range: 0...50, step: 5,
                           format: { "\(Int($0))" }) { v in Task { await store.set("max_backlog", String(Int(v))) } }
            }
        }
    }

    private var files: some View {
        Group {
            SectionTitle(text: "Files")
            Card {
                LinkRow(title: "Open config.toml", symbol: "doc.text") { NSWorkspace.shared.open(companion.configURL) }
                RowDivider()
                LinkRow(title: "Open Log", symbol: "list.bullet.rectangle") { NSWorkspace.shared.open(companion.logURL) }
                RowDivider()
                LinkRow(title: "Open DOINK Folder", symbol: "folder") { NSWorkspace.shared.open(companion.folder) }
                RowDivider()
                LinkRow(title: "Restart Companion", symbol: "arrow.clockwise") { companion.restart() }
                RowDivider()
                LinkRow(title: "Quit DOINK", symbol: "power", tint: Palette.red) { NSApp.terminate(nil) }
            }
        }
    }

    // MARK: Helpers

    private func boolBinding(_ key: String, default def: Bool) -> Binding<Bool> {
        Binding(get: { store.bool(key, default: def) },
                set: { v in Task { await store.set(key, v) } })
    }

    private func chooseFolder() {
        let open = NSOpenPanel()
        open.canChooseDirectories = true
        open.canChooseFiles = false
        open.allowsMultipleSelection = false
        open.message = "Choose your World of Warcraft folder (the one that contains _forever_)."
        open.directoryURL = URL(fileURLWithPath: "/Applications")
        open.level = NSWindow.Level(rawValue: NSWindow.Level.popUpMenu.rawValue + 1)
        NSApp.activate(ignoringOtherApps: true)
        if open.runModal() == .OK, let url = open.url {
            Task { await store.set("wow_dir", url.path) }
        }
    }

    private func setLaunchAtLogin(_ on: Bool) {
        do {
            if on { try SMAppService.mainApp.register() } else { try SMAppService.mainApp.unregister() }
            store.message = nil
        } catch {
            store.message = ("Couldn't change Launch at Login: \(error.localizedDescription). Move DOINK.app to Applications and try again.", true)
        }
        launchAtLogin = SMAppService.mainApp.status == .enabled
    }
}

// MARK: - Rows

extension Text {
    func rowTitle() -> some View {
        font(.system(size: 13)).foregroundStyle(Palette.primary)
    }
}

struct RowDivider: View {
    var body: some View { Rectangle().fill(Palette.divider).frame(height: 0.5) }
}

struct Caption: View {
    var text: String
    var body: some View {
        Text(text).font(.system(size: 11)).foregroundStyle(Palette.secondary)
            .fixedSize(horizontal: false, vertical: true)
    }
}

struct ToggleRow: View {
    var title: String
    @Binding var isOn: Bool
    var body: some View {
        HStack {
            Text(title).rowTitle()
            Spacer()
            Toggle(title, isOn: $isOn).labelsHidden() // switch at the right edge, as in OpenUsage
                .toggleStyle(.switch).tint(Palette.blue)
        }
    }
}

struct PickerRow<Option: Hashable & Identifiable & RawRepresentable>: View where Option.RawValue == String {
    var title: String
    @Binding var selection: Option
    var options: [Option]
    var body: some View {
        HStack {
            Text(title).rowTitle()
            Spacer()
            Picker("", selection: $selection) {
                ForEach(options) { Text($0.rawValue).tag($0) }
            }
            .pickerStyle(.menu).labelsHidden().fixedSize()
        }
    }
}

struct StepperRow: View {
    var title: String
    var value: Double
    var range: ClosedRange<Double>
    var step: Double
    var format: (Double) -> String
    var commit: (Double) -> Void
    var body: some View {
        HStack {
            Text(title).rowTitle()
            Spacer()
            Text(format(value)).font(.system(size: 13)).foregroundStyle(Palette.secondary).monospacedDigit()
            Stepper("", value: Binding(get: { value }, set: { commit(min(range.upperBound, max(range.lowerBound, $0))) }),
                    in: range, step: step)
                .labelsHidden()
        }
    }
}

struct LinkRow: View {
    var title: String
    var symbol: String
    var tint: Color = Palette.primary
    var action: () -> Void
    var body: some View {
        Button(action: action) {
            HStack(spacing: 10) {
                Image(systemName: symbol).font(.system(size: 13)).foregroundStyle(tint).frame(width: 18)
                Text(title).font(.system(size: 13)).foregroundStyle(tint)
                Spacer()
                Image(systemName: "chevron.right").font(.system(size: 11, weight: .semibold)).foregroundStyle(Palette.tertiary)
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }
}

/// A URL setting: shown masked (the token stays hidden, like in game);
/// "Change" opens a field with Save, Test and Remove.
struct URLRow: View {
    var title: String
    var placeholder: String
    var value: String
    var save: (String) async -> Bool
    var test: ((String) async -> Void)?

    @State private var editing = false
    @State private var draft = ""
    @State private var busy = false

    var body: some View {
        VStack(alignment: .leading, spacing: 8) {
            HStack(alignment: .firstTextBaseline) {
                Text(title).rowTitle()
                Spacer()
                if !editing {
                    Text(masked).font(.system(size: 12)).foregroundStyle(Palette.secondary).lineLimit(1)
                    Button(value.isEmpty ? "Add" : "Change") { draft = ""; editing = true }.buttonStyle(PillButton())
                }
            }
            if editing {
                TextField(placeholder, text: $draft)
                    .textFieldStyle(.plain)
                    .font(.system(size: 12))
                    .padding(.horizontal, 10).padding(.vertical, 7)
                    .background(RoundedRectangle(cornerRadius: 6, style: .continuous).fill(Palette.track))
                    .onSubmit(commit)
                HStack(spacing: 6) {
                    Button("Save", action: commit).buttonStyle(PillButton(prominent: true)).disabled(busy || draft.isEmpty)
                    if let test {
                        Button("Test") { run { await test(draft.trimmingCharacters(in: .whitespaces)) } }
                            .buttonStyle(PillButton()).disabled(busy)
                    }
                    Spacer()
                    if !value.isEmpty {
                        Button("Remove") { run { if await save("") { editing = false } } }
                            .buttonStyle(PillButton(destructive: true)).disabled(busy)
                    }
                    Button("Cancel") { editing = false }.buttonStyle(PillButton())
                }
            } else if let test, !value.isEmpty {
                Button("Send Test Message") { run { await test("") } }.buttonStyle(PillButton()).disabled(busy)
            }
        }
    }

    private var masked: String {
        guard !value.isEmpty, let url = URL(string: value), let host = url.host else { return value.isEmpty ? "Not set" : "Set" }
        return "\(host) ····\(value.suffix(4))"
    }

    private func commit() {
        let v = draft.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !v.isEmpty else { return }
        run { if await save(v) { editing = false } }
    }

    private func run(_ work: @escaping () async -> Void) {
        busy = true
        Task {
            await work()
            busy = false
        }
    }
}

/// OpenUsage's small grey pill buttons ("Install Update", "Status ↗").
struct PillButton: ButtonStyle {
    var prominent = false
    var destructive = false
    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.system(size: 12, weight: .medium))
            .foregroundStyle(prominent ? Color.white : destructive ? Palette.red : Palette.primary)
            .padding(.horizontal, 10).padding(.vertical, 4)
            .background(RoundedRectangle(cornerRadius: 6, style: .continuous)
                .fill(prominent ? Palette.blue : Palette.track))
            .opacity(configuration.isPressed ? 0.7 : 1)
    }
}
