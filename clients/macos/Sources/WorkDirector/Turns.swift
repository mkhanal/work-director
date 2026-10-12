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
        case .supervisor: "Supervisor"
        case .plan: "Plan"
        case .director: "Director"
        case .task(let w): w.title
        case .agent(let name): name
        }
    }

    @ViewBuilder var mark: some View {
        switch self {
        case .you:
            Image(systemName: "person.crop.circle.fill").foregroundStyle(Color.accentColor)
        case .supervisor:
            Image(systemName: "person.badge.shield.checkmark.fill").foregroundStyle(.secondary)
        case .plan:
            Image(systemName: "list.bullet.rectangle.fill").foregroundStyle(.secondary)
        case .director:
            Image(systemName: "checkmark.seal.fill").foregroundStyle(Palette.settled)
        case .task(let w):
            Image(systemName: w.state.look.symbol).foregroundStyle(w.state.look.tint.color)
        case .agent:
            Image(systemName: "sparkle").foregroundStyle(Color.accentColor)
        }
    }
}

/// One turn of a conversation: the speaker's mark, their name and when, then
/// what they said, never under the mark.
struct Turn<Content: View, Detail: View>: View {
    let speaker: Speaker
    let at: String?
    /// Opens the speaker's own conversation, when it has one.
    var open: (() -> Void)? = nil
    @ViewBuilder var detail: Detail
    @ViewBuilder var content: Content

    var body: some View {
        HStack(alignment: .top, spacing: Space.s3) {
            speaker.mark
                .font(.system(size: IconSize.avatar))
                .frame(width: Space.avatar, height: 18)
            VStack(alignment: .leading, spacing: Space.s2) {
                HStack(spacing: Space.s1 + 2) {
                    if let open {
                        Button(speaker.name, action: open)
                            .buttonStyle(.plain)
                            .foregroundStyle(.primary)
                            .help("Show this conversation")
                    } else {
                        Text(speaker.name).foregroundStyle(.primary)
                    }
                    if let at { Text(when(at)).foregroundStyle(.tertiary) }
                    detail
                }
                .font(TypeScale.meta)
                .lineLimit(1)
                content
            }
            .frame(maxWidth: .infinity, alignment: .leading)
        }
    }
}

extension Turn where Detail == EmptyView {
    init(speaker: Speaker, at: String?, open: (() -> Void)? = nil, @ViewBuilder content: () -> Content) {
        self.init(speaker: speaker, at: at, open: open, detail: { EmptyView() }, content: content)
    }
}

/// Something that happened rather than something said: a look, a state
/// change, a session that stopped. Centred between hairlines.
struct EventLine: View {
    let text: String
    var symbol: String? = nil
    var at: String? = nil
    var tint: Color = .secondary

    var body: some View {
        HStack(spacing: Space.s3) {
            Rectangle().fill(Palette.separator).frame(height: 1)
            HStack(spacing: Space.s1 + 2) {
                if let symbol { Image(systemName: symbol).foregroundStyle(tint) }
                Text(text).lineLimit(1).truncationMode(.middle)
                if let at { Text("· " + when(at)) }
            }
            .font(TypeScale.meta)
            .foregroundStyle(.tertiary)
            .fixedSize(horizontal: false, vertical: true)
            .layoutPriority(1)
            Rectangle().fill(Palette.separator).frame(height: 1)
        }
        .padding(.vertical, Space.s1)
    }
}
