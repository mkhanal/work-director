import AppKit
import SwiftUI
import WorkDirectorKit

@main
struct WorkDirectorApp: App {
    @State private var launch = Launch()

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
        }
        .defaultSize(width: 1180, height: 760)
        .commands {
            CommandGroup(after: .newItem) {
                Button("Run a wd Command…") { NotificationCenter.default.post(name: .openCommandBar, object: nil) }
                    .keyboardShortcut("k", modifiers: .command)
            }
            CommandGroup(after: .toolbar) {
                Button("Reload") { NotificationCenter.default.post(name: .reload, object: nil) }
                    .keyboardShortcut("r", modifiers: .command)
            }
        }
    }
}

extension Notification.Name {
    static let openCommandBar = Notification.Name("openCommandBar")
    static let reload = Notification.Name("reload")
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
            let store = Store(channel: try Channel(wd: try locateWD()))
            state = .running(store)
            await store.start()
        } catch {
            state = .failed("\(error)")
        }
    }
}
