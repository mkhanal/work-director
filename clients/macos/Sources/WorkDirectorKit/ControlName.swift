import Foundation

/// The words each control shows and is announced by to assistive technology,
/// the same as the Linux app's.
public enum ControlName {
    public static let approve = "Approve and start"
    public static let discard = "Discard"
    public static let replan = "Re-plan with a note…"
    public static let answer = "Answer"
    public static let directorDecides = "Let the director decide"
    public static let editChange = "Edit the change…"
    public static let somethingElse = "Something else"

    /// The button that answers with a change's first choice.
    public static func recommended(_ choice: String) -> String { choice + " · Recommended" }

    /// A proposed card's action, named with its card: "Split 2 · <title>".
    public static func card(_ action: String, _ row: ProposedRow, then: String = "") -> String {
        "\(action) \(row.number) · \(row.node.work.title)\(then)"
    }

    /// The finish-line menu, by the line it shows.
    public static func finishMenu(_ shown: String?) -> String {
        "Finishes at: " + (shown ?? "Not decided yet")
    }

    /// A question's option, by the title it shows: a finish line by its
    /// meaning's title, any other by its label without a recommended mark.
    public static func option(_ label: String, finish: Bool) -> String {
        let bare = AnswerDraft.unmarked(label)
        return finish ? FinishMeaning.of(bare)?.title ?? bare : bare
    }
}
