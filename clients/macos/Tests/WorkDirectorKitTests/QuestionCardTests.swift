import AppKit
import SwiftUI
import Testing
@testable import WorkDirector
@testable import WorkDirectorKit

@MainActor
struct QuestionCardTests {
    func asking(_ id: String, _ parts: Int) -> Question {
        Question(id: id, items: (0..<parts).map { n in
            QuestionItem(question: "Part \(n + 1)?", options: [QuestionOption(label: "Yes"), QuestionOption(label: "No")])
        })
    }

    /// The same place on screen asked another question, with more parts than
    /// the one before: the card starts the new one afresh.
    @Test func aCardShowingAnotherQuestionStartsItAfresh() throws {
        let store = Store(channel: try Home().channel())
        func card(_ question: Question) -> AnyView {
            AnyView(QuestionCard(store: store, workID: "w", question: question, answered: {}))
        }
        let host = NSHostingView(rootView: card(asking("q1", 1)))
        host.setFrameSize(NSSize(width: 600, height: 400))
        host.layoutSubtreeIfNeeded()
        host.rootView = card(asking("q2", 3))
        host.layoutSubtreeIfNeeded()
        #expect(host.fittingSize.height > 0)
    }
}
