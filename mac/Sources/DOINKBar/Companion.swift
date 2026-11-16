import AppKit
import Foundation

/// Runs the Go companion (`doink`, bundled in the app's Resources) as a
/// child process and reads the status file it keeps. Everything the
/// companion does on Windows (watching DOINK.lua, posting to Discord,
/// realtime screenshots) happens in that process; this class only starts,
/// restarts and stops it.
///
/// Files live in ~/Library/Application Support/DOINK: config.toml,
/// state.json, doink.log and status.json.
@MainActor
final class Companion: ObservableObject {
    enum Phase: Equatable {
        case starting
        case running
        case stopped(String) // why
    }

    @Published private(set) var status: CompanionStatus?
    @Published private(set) var phase: Phase = .starting
    @Published private(set) var now = Date()

    let folder: URL
    var configURL: URL { folder.appendingPathComponent("config.toml") }
    var logURL: URL { folder.appendingPathComponent("doink.log") }
    var statusURL: URL { folder.appendingPathComponent("status.json") }

    private var process: Process?
    private var stopping = false
    private var restarts = 0
    private var timer: Timer?

    init() {
        let support = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
        folder = support.appendingPathComponent("DOINK", isDirectory: true)
        try? FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        writeConfigTemplate()
        start()
        timer = Timer.scheduledTimer(withTimeInterval: 1, repeats: true) { [weak self] _ in
            Task { @MainActor in self?.tick() }
        }
        NotificationCenter.default.addObserver(forName: NSApplication.willTerminateNotification,
                                               object: nil, queue: .main) { [weak self] _ in
            MainActor.assumeIsolated { self?.stop() }
        }
    }

    /// The `doink` binary: $DOINK_BIN (development), else the app bundle's.
    var binary: URL? {
        if let env = ProcessInfo.processInfo.environment["DOINK_BIN"], !env.isEmpty {
            return URL(fileURLWithPath: env)
        }
        return Bundle.main.url(forResource: "doink", withExtension: nil)
    }

    func start() {
        guard process == nil else { return }
        guard let binary, FileManager.default.isExecutableFile(atPath: binary.path) else {
            phase = .stopped("The doink companion is missing from the app. Rebuild it with mac/build.sh.")
            return
        }
        stopping = false
        rotateLog()
        let p = Process()
        p.executableURL = binary
        p.arguments = ["--console", "--config", configURL.path, "--status-file", statusURL.path]
        p.currentDirectoryURL = folder
        if !FileManager.default.fileExists(atPath: logURL.path) {
            FileManager.default.createFile(atPath: logURL.path, contents: nil)
        }
        if let log = try? FileHandle(forWritingTo: logURL) {
            log.seekToEndOfFile()
            p.standardOutput = log
            p.standardError = log
        }
        p.terminationHandler = { [weak self] proc in
            let code = proc.terminationStatus
            Task { @MainActor in self?.exited(code) }
        }
        do {
            try p.run()
            process = p
            phase = .starting
        } catch {
            phase = .stopped("Couldn't start the companion: \(error.localizedDescription)")
        }
    }

    func stop() {
        stopping = true
        guard let p = process else { return }
        p.terminate() // SIGTERM: the companion removes its status file
        let deadline = Date().addingTimeInterval(2)
        while p.isRunning && Date() < deadline { usleep(50_000) }
        process = nil
    }

    func restart() {
        stop()
        restarts = 0
        status = nil
        start()
    }

    private func exited(_ code: Int32) {
        process = nil
        status = nil
        if stopping { return }
        restarts += 1
        let delay = min(60.0, pow(2.0, Double(min(restarts, 6))))
        phase = .stopped("The companion stopped (exit code \(code)). Restarting in \(Int(delay))s; see doink.log.")
        DispatchQueue.main.asyncAfter(deadline: .now() + delay) { [weak self] in
            guard let self, !self.stopping, self.process == nil else { return }
            self.start()
        }
    }

    /// Read the status file now rather than at the next tick.
    func panelOpened() { tick() }

    /// Runs a one-shot `doink` command (config show/set, --test-webhook)
    /// off the main thread with this app's config.toml.
    nonisolated func run(_ args: [String], binary: URL?, config: URL) async -> (code: Int32, out: String, err: String) {
        guard let binary else { return (-1, "", "The doink companion is missing from the app.") }
        return await withCheckedContinuation { cont in
            DispatchQueue.global(qos: .userInitiated).async {
                let p = Process()
                p.executableURL = binary
                p.arguments = args + ["--config", config.path]
                let out = Pipe(), err = Pipe()
                p.standardOutput = out
                p.standardError = err
                do { try p.run() } catch {
                    cont.resume(returning: (-1, "", error.localizedDescription))
                    return
                }
                let o = out.fileHandleForReading.readDataToEndOfFile()
                let e = err.fileHandleForReading.readDataToEndOfFile()
                p.waitUntilExit()
                cont.resume(returning: (p.terminationStatus, String(decoding: o, as: UTF8.self),
                                        String(decoding: e, as: UTF8.self)))
            }
        }
    }

    private func tick() {
        now = Date()
        // Only our own child's file counts (not one left by a crash).
        guard let pid = process?.processIdentifier,
              let data = try? Data(contentsOf: statusURL),
              let s = try? JSONDecoder().decode(CompanionStatus.self, from: data),
              s.pid == Int(pid) else { return }
        if s != status { status = s }
        if phase != .running {
            phase = .running
            restarts = 0
        }
    }

    /// doink.log -> doink.log.1 past 1 MB, as on Windows.
    private func rotateLog() {
        let fm = FileManager.default
        guard let size = (try? fm.attributesOfItem(atPath: logURL.path))?[.size] as? Int, size > 1_000_000 else { return }
        let old = folder.appendingPathComponent("doink.log.1")
        try? fm.removeItem(at: old)
        try? fm.moveItem(at: logURL, to: old)
    }

    private func writeConfigTemplate() {
        guard !FileManager.default.fileExists(atPath: configURL.path) else { return }
        let template = """
        # DOINK companion settings. Every key is optional; see config.example.toml.
        # The webhook is usually set in game: /doink webhook <url>
        # webhook_url = "https://discord.com/api/webhooks/..."
        # wow_dir = '/Applications/World of Warcraft'

        """
        try? template.write(to: configURL, atomically: true, encoding: .utf8)
    }
}
