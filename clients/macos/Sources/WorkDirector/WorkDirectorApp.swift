import AppKit
import SwiftUI
import WorkDirectorKit

@main
struct WorkDirectorApp: App {
    @State private var launch = Launch()
    @AppStorage("appearance") private var appearance: AppearanceChoice = .system

    static let background = UserDefaults.standard.bool(forKey: "ui.background")

    init() {
        // Run from `swift run` there is no bundle to make the app a regular,
        // frontmost application; inside Work Director.app this is already so.
        // A UI check launches it with `-ui.background YES`: it may then never
        // become active, so it can never take the screen or the keystrokes
        // of the person using the Mac.
        NSApplication.shared.setActivationPolicy(Self.background ? .prohibited : .regular)
        if Self.background {
            // Its window goes behind every other, so it covers nothing the
            // person is looking at; a check photographs it all the same.
            NotificationCenter.default.addObserver(forName: NSWindow.didUpdateNotification, object: nil, queue: .main) { note in
                guard let window = note.object as? NSWindow else { return }
                MainActor.assumeIsolated { BackWindows.send(window) }
            }
        }
        // A write to a wd that has exited must fail as an error the window can
        // show; the default SIGPIPE would end the app instead.
        signal(SIGPIPE, SIG_IGN)
        Self.keepCrashNotes()
    }

    /// Crashes leave notes in $WD_HOME/crashes for `wd crashes`: what was on
    /// screen and done is kept as the app runs, an exception writes its note
    /// as it is thrown, and each launch pairs the last run's crash with the
    /// system's report of it.
    private static func keepCrashNotes() {
        let home = CrashLog.home()
        let log = CrashLog(home: home, build: CrashLog.build(of: .main))
        CrashLog.current = log
        do {
            try CrashLog.catchExceptions()
            try log.write()
        } catch {
            FileHandle.standardError.write(Data("crash notes: \(error)\n".utf8))
        }
        NotificationCenter.default.addObserver(forName: NSApplication.willTerminateNotification, object: nil, queue: nil) { _ in
            do {
                try log.ended()
            } catch {
                FileHandle.standardError.write(Data("crash notes: \(error)\n".utf8))
            }
        }
        let reports = FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent("Library/Logs/DiagnosticReports")
        Task.detached(priority: .utility) {
            do {
                for dir in try CrashLog.gather(home: home, reports: reports, alive: { kill($0, 0) == 0 || errno == EPERM }) {
                    FileHandle.standardError.write(Data("the last run crashed: wd crashes \(dir.lastPathComponent)\n".utf8))
                }
            } catch {
                FileHandle.standardError.write(Data("crash notes: pairing the last run's crash: \(error)\n".utf8))
            }
        }
    }

    var body: some Scene {
        Window("Work Director", id: "main") {
            Group {
                switch launch.state {
                case .starting:
                    ProgressView("Starting wd…").frame(minWidth: 480, minHeight: 320)
                case .failed(let reason):
                    ContentUnavailableView("Work Director cannot start", systemImage: "exclamationmark.triangle", description: Text(reason))
                        .frame(minWidth: 480, minHeight: 320)
                case .running(let store):
                    RootView(store: store)
                }
            }
            .task { await launch.start() }
            .onChange(of: appearance, initial: true) { _, choice in choice.apply() }
        }
        .defaultSize(width: 1180, height: 760)
        .commands {
            CommandGroup(replacing: .newItem) {
                Button("New Goal…") { NotificationCenter.default.post(name: .newGoal, object: nil) }
                    .keyboardShortcut("n", modifiers: .command)
            }
            CommandGroup(after: .newItem) {
                Button("Add Product…") { NotificationCenter.default.post(name: .addProduct, object: nil) }
                Button("Run a wd Command…") { NotificationCenter.default.post(name: .openCommandBar, object: nil) }
                    .keyboardShortcut("k", modifiers: .command)
            }
            CommandGroup(after: .toolbar) {
                Button("Reload") { NotificationCenter.default.post(name: .reload, object: nil) }
                    .keyboardShortcut("r", modifiers: .command)
            }
            FindCommands()
        }
        Settings {
            Preferences()
        }
    }
}

/// The windows a background launch has sent behind every other, once each.
@MainActor
enum BackWindows {
    private static var sent: Set<ObjectIdentifier> = []

    static func send(_ window: NSWindow) {
        guard window.isVisible, !window.isSheet, sent.insert(ObjectIdentifier(window)).inserted else { return }
        window.orderBack(nil)
    }
}

extension Notification.Name {
    static let openCommandBar = Notification.Name("openCommandBar")
    static let reload = Notification.Name("reload")
    static let newGoal = Notification.Name("newGoal")
    static let addProduct = Notification.Name("addProduct")
    /// Opens one work item; its id is the notification's object.
    static let openWork = Notification.Name("openWork")
}

@MainActor
@Observable
final class Launch {
    enum State {
        case starting
        case failed(String)
        case running(Store)
    }

    private(set) var state: State = .starting

    func start() async {
        guard case .starting = state else { return }
        do {
            let wd = try locateWD()
            let environment = await Task.detached { LoginPath.resolve() }.value
            let store = Store(channel: try Channel(wd: wd, environment: environment, trail: { CrashLog.current?.did($0) }))
            Notifier.shared.start()
            store.told = { Notifier.shared.announce($0) }
            state = .running(store)
            await store.start()
        } catch {
            state = .failed("\(error)")
        }
    }
}

extension AppearanceChoice {
    /// Sets the whole app's look; following the system clears it.
    @MainActor func apply() {
        NSApp.appearance = dark.map { NSAppearance(named: $0 ? .darkAqua : .aqua) } ?? nil
    }
}

/// What a person can set about how Work Director behaves for them.
struct Preferences: View {
    @AppStorage("appearance") private var appearance: AppearanceChoice = .system
    @AppStorage("sendKey") private var sendKey: SendKey = .standard
    @AppStorage("notify") private var notify = true

    var body: some View {
        Form {
            Picker("Appearance", selection: $appearance) {
                ForEach(AppearanceChoice.allCases, id: \.self) { Text($0.title).tag($0) }
            }
            .pickerStyle(.segmented)
            Picker("Send a message with", selection: $sendKey) {
                ForEach(SendKey.allCases) { key in Text(key.title).tag(key) }
            }
            .pickerStyle(.radioGroup)
            Text("Any other chord with Return starts a new line. Esc pressed twice stops a session while it works.")
                .font(TypeScale.caption)
                .foregroundStyle(.secondary)
            Toggle("Tell me when work needs me or reaches a gate", isOn: $notify)
        }
        .padding(Space.s5)
        .frame(width: 460)
    }
}
