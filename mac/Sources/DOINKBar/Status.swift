import Foundation

/// What `doink --status-file` writes (companion/cmd/doink/statusfile.go).
struct CompanionStatus: Decodable, Equatable {
    var version: String
    var pid: Int
    var updated: TimeInterval
    var every: Int
    var dryRun: Bool
    var watching: Int
    var files: [String]
    var error: String
    var line: String
    var recent: [Post]
    var toons: Toons

    struct Post: Decodable, Equatable, Hashable {
        var at: TimeInterval
        var who: String
        var what: String
    }

    struct Toons: Decodable, Equatable {
        var last: Toon?
        var others: [Toon]
    }

    enum CodingKeys: String, CodingKey {
        case version, pid, updated, every, watching, files, error, line, recent, toons
        case dryRun = "dry_run"
    }
}

struct Toon: Decodable, Equatable, Identifiable {
    var name: String
    var surname: String?
    var realm: String?
    var `class`: String?
    var level: Int?
    var lastSeen: TimeInterval?
    var played: Int?
    var counts: [String: Int]
    var recent: [ToonEvent]

    var id: String { fullName + "-" + (realm ?? "") }
    var fullName: String { [name, surname].compactMap { $0 }.joined(separator: " ") }

    /// "Warlock", or nil when DOINK hasn't seen the class yet.
    var className: String? {
        guard let c = `class`, !c.isEmpty else { return nil }
        return c == "DEATHKNIGHT" ? "Death Knight" : c.capitalized
    }

    /// "Level 28 Warlock", "Level 28", "Warlock" or "Not recorded yet".
    var subtitle: String {
        switch (level, className) {
        case let (l?, c?): return "Level \(l) \(c)"
        case let (l?, nil): return "Level \(l)"
        case let (nil, c?): return c
        default: return "No DOINK events yet"
        }
    }

    enum CodingKeys: String, CodingKey {
        case name, surname, realm, `class`, level, played, counts, recent
        case lastSeen = "last_seen"
    }
}

struct ToonEvent: Decodable, Equatable, Hashable {
    var type: String
    var ts: TimeInterval
    var text: String

    /// SF Symbol for the event type.
    var symbol: String {
        switch type {
        case "level_up": return "arrow.up.circle.fill"
        case "quest": return "exclamationmark.bubble.fill"
        case "death": return "xmark.octagon.fill"
        case "boss_kill": return "crown.fill"
        case "loot": return "bag.fill"
        case "skill_up": return "hammer.fill"
        case "mount": return "hare.fill"
        case "duel": return "figure.fencing"
        case "battleground": return "flag.2.crossed.fill"
        default: return "sparkle"
        }
    }
}

enum Format {
    /// "now", "42s", "5m", "3h", "2d" (like OpenUsage's reset times).
    static func ago(_ ts: TimeInterval, now: Date = Date()) -> String {
        let s = max(0, Int(now.timeIntervalSince1970 - ts))
        switch s {
        case ..<10: return "now"
        case ..<60: return "\(s)s"
        case ..<3600: return "\(s / 60)m"
        case ..<86400: return "\(s / 3600)h"
        default: return "\(s / 86400)d"
        }
    }

    /// "4d 17h 27m".
    static func played(_ seconds: Int) -> String {
        let d = seconds / 86400, h = seconds % 86400 / 3600, m = seconds % 3600 / 60
        if d > 0 { return "\(d)d \(h)h \(m)m" }
        if h > 0 { return "\(h)h \(m)m" }
        return "\(m)m"
    }

    /// level_up -> "Level up".
    static func type(_ t: String) -> String {
        switch t {
        case "level_up": return "Level up"
        case "boss_kill": return "Boss kill"
        case "skill_up": return "Skill up"
        default: return t.prefix(1).uppercased() + t.dropFirst()
        }
    }
}
