import AppKit
import SwiftUI
import WorkDirectorKit

@main
struct WorkDirectorApp: App {
    @State private var launch = Launch()
    @AppStorage("appearance") private var appearance: AppearanceChoice = .system

    init() {
        // Run from `swift run` there is no bundle to make the app a regular,
        // frontmost application; inside Work Director.app this is already so.
        NSApplication.shared.setActivationPolicy(.regular)
        // A write to a wd that has exited must fail as an error the window can
        // show; the default SIGPIPE would end the app instead.
        signal(SIGPIPE, SIG_IGN)
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
        }
        Settings {
            Preferences()
        }
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
            let store = Store(channel: try Channel(wd: wd, environment: environment))
            Notifier.shared.start()
            store.arrived = { Notifier.shared.announce($0) }
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
