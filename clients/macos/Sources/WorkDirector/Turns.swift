import SwiftUI
import WorkDirectorKit

/// Who said something in a conversation, and the mark beside their words.
enum Speaker {
    case you
    case supervisor
    case plan
    case director
    /// A task, by its row.
    case task(Work)
    /// An agent's own session, by name.
    case agent(String)

    var name: String {
        switch self {
        case .you: "You"
        case .supervisor: "Director"
        case .plan: "Director"
        case .director: "Director"
        case .task(let w): w.runner ?? w.title
        case .agent(let name): name
        }
    }

    /// The canvas's avatars: the director as a settled check, an agent as
    /// its sub-goal's state.
    @ViewBuilder var mark: some View {
        switch self {
        case .you:
            Image(systemName: "person.crop.circle.fill").foregroundStyle(Palette.accent)
        case .supervisor, .plan, .director:
            StateIcon(mark: .done, size: Space.avatar)
        case .task(let w):
            StateIcon(mark: w.state.mark, size: Space.avatar)
        case .agent:
            StateIcon(mark: .working, size: Space.avatar)
        }
    }
}

/// One turn of a conversation: the speaker's mark, their name, the sub-goal
/// it was said in and when, then what they said, never under the mark.
struct Turn<Content: View, Detail: View>: View {
    let speaker: Speaker
    let at: String?
    /// Opens the speaker's own conversation, when it has one.
    var open: (() -> Void)? = nil
    /// Where in the goal's tree it was said.
    var tag: String? = nil
    @ViewBuilder var detail: Detail
    @ViewBuilder var content: Content

    var body: some View {
        HStack(alignment: .top, spacing: Space.s3) {
            speaker.mark
                .font(.system(size: IconSize.avatar))
                .frame(width: Space.avatar, height: Space.avatar)
            VStack(alignment: .leading, spacing: Space.s2) {
                HStack(spacing: 6) {
                    if let open {
                        Button(speaker.name, action: open)
                            .buttonStyle(.plain)
                            .help("Show this conversation")
                    } else {
                        Text(speaker.name)
                    }
                    if let tag { SubGoalTag(text: tag) }
                    if let at { Text("· " + when(at)) }
                    detail
                }
                .font(TypeScale.meta)
                .foregroundStyle(Palette.text2)
                .lineLimit(1)
                .frame(minHeight: 18)
                content
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}

extension Turn where Detail == EmptyView {
    init(speaker: Speaker, at: String?, open: (() -> Void)? = nil, tag: String? = nil, @ViewBuilder content: () -> Content) {
        self.init(speaker: speaker, at: at, open: open, tag: tag, detail: { EmptyView() }, content: content)
    }
}

/// Something that happened rather than something said: a sub-goal finishing,
/// a look, a state change. Centred between hairlines.
struct EventLine: View {
    let text: String
    var symbol: String? = nil
    var at: String? = nil
    var tint: Color = Palette.text3

    var body: some View {
        HStack(spacing: 10) {
            Rectangle().fill(Palette.hairline).frame(height: 1)
            HStack(spacing: Space.s1 + 2) {
                if let symbol { Image(systemName: symbol).foregroundStyle(tint) }
                Text(text).lineLimit(1).truncationMode(.middle)
                if let at { Text("· " + when(at)) }
            }
            .font(TypeScale.caption)
            .foregroundStyle(Palette.text3)
            .fixedSize(horizontal: false, vertical: true)
            .layoutPriority(1)
            Rectangle().fill(Palette.hairline).frame(height: 1)
        }
        .padding(.vertical, Space.s1)
    }
}
