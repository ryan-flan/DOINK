import Foundation

/// The companion's settings, read and written through `doink config
/// show|set` so config.toml keeps its comments and every value is checked
/// by the same code that loads it. The running companion reloads
/// config.toml by itself within a few seconds; the few settings it only
/// reads at start-up restart it.
@MainActor
final class SettingsStore: ObservableObject {
    /// What `doink config show` prints (cmd/doink/configcmd.go).
    struct View: Decodable {
        var values: [String: JSONValue]
        var found: [String]
        var wowDirs: [String]
        var gameWebhooks: String
        var routes: [String: String]
        var error: String

        enum CodingKeys: String, CodingKey {
            case values, found, error, routes
            case wowDirs = "wow_dirs"
            case gameWebhooks = "game_webhooks"
        }
    }

    enum JSONValue: Decodable, Equatable {
        case string(String), bool(Bool), number(Double)
        init(from decoder: Decoder) throws {
            let c = try decoder.singleValueContainer()
            if let b = try? c.decode(Bool.self) { self = .bool(b) }
            else if let d = try? c.decode(Double.self) { self = .number(d) }
            else { self = .string(try c.decode(String.self)) }
        }
    }

    /// Settings read only at start-up (or whose file list the menu bar
    /// app's status needs): changing one restarts the companion.
    static let restartKeys: Set<String> = ["dry_run", "wowhead", "wow_dir"]

    @Published var view: View?
    @Published private(set) var loading = false
    @Published var message: (text: String, isError: Bool)?

    private let companion: Companion

    init(companion: Companion) {
        self.companion = companion
    }

    /// The event types that can have their own webhook ([webhooks] in
    /// config.toml), with their labels, in the addon's order.
    static let actions: [(type: String, label: String)] = [
        ("level_up", "Level Up"), ("loot", "Loot"), ("death", "Death"), ("quest", "Quest"),
        ("boss_kill", "Boss Kill"), ("skill_up", "Skill Up"), ("mount", "Mount"),
        ("duel", "Duel"), ("battleground", "Battleground"),
    ]

    /// The webhook an event type is routed to, or "".
    func route(_ type: String) -> String { view?.routes[type] ?? "" }

    // MARK: Values (absent = the companion's default)

    func string(_ key: String) -> String {
        if case let .string(s)? = view?.values[key] { return s }
        return ""
    }

    func bool(_ key: String, default def: Bool) -> Bool {
        if case let .bool(b)? = view?.values[key] { return b }
        return def
    }

    func number(_ key: String, default def: Double) -> Double {
        if case let .number(n)? = view?.values[key] { return n }
        return def
    }

    // MARK: Commands

    func load() async {
        loading = true
        defer { loading = false }
        let r = await companion.run(["config", "show"], binary: companion.binary, config: companion.configURL)
        guard r.code == 0, let data = r.out.data(using: .utf8),
              let v = try? JSONDecoder().decode(View.self, from: data) else {
            message = (firstLine(r.err) ?? "Couldn't read the settings.", true)
            return
        }
        view = v
    }

    /// Writes key = value ("" restores the default). Returns false and
    /// sets `message` when the companion refuses it.
    @discardableResult
    func set(_ key: String, _ value: String) async -> Bool {
        let r = await companion.run(["config", "set", "\(key)=\(value)"], binary: companion.binary, config: companion.configURL)
        guard r.code == 0 else {
            message = (capitalized(firstLine(r.err) ?? "Couldn't save \(key)."), true)
            return false
        }
        message = nil
        await load()
        if Self.restartKeys.contains(key) {
            companion.restart()
        }
        return true
    }

    func set(_ key: String, _ value: Bool) async { await set(key, value ? "true" : "false") }

    /// Posts the connection-test message to url, or to the webhook DOINK
    /// would use when url is empty.
    func test(_ url: String) async {
        message = ("Sending a test message…", false)
        var args = ["--test-webhook"]
        if !url.isEmpty { args.append(url) }
        let r = await companion.run(args, binary: companion.binary, config: companion.configURL)
        if r.code == 0 {
            message = ("Test message posted. Check your Discord channel.", false)
        } else {
            // "15:04:05 ERROR   no webhook to test..." -> the message.
            let line = r.err.split(separator: "\n").last { $0.contains("ERROR") }.map(String.init) ?? r.err
            let text = line.components(separatedBy: "ERROR").last?.trimmingCharacters(in: .whitespaces) ?? line
            message = (capitalized(text.isEmpty ? "The test message failed." : text), true)
        }
    }

    private func firstLine(_ s: String) -> String? {
        s.split(separator: "\n").first.map { String($0).trimmingCharacters(in: .whitespaces) }
    }

    private func capitalized(_ s: String) -> String {
        s.prefix(1).uppercased() + s.dropFirst()
    }
}
