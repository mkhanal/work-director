import SwiftUI
import WorkDirectorKit

/// Who is working, on what, for how long, live at the end of a conversation.
struct WorkingIndicator: View {
    let rows: WorkingRows

    var body: some View {
        if !rows.isEmpty {
            VStack(alignment: .leading, spacing: Space.s2) {
                ForEach(rows.shown) { row in
                    HStack(alignment: .firstTextBaseline, spacing: Space.s2) {
                        WorkingMark().alignmentGuide(.firstTextBaseline) { $0[VerticalAlignment.center] + 4 }
                        Text(row.who).font(TypeScale.calloutMedium).foregroundStyle(Palette.text).lineLimit(1)
                        Shimmer(text: row.doing)
                            .lineLimit(1)
                            .truncationMode(.middle)
                            .layoutPriority(1)
                        Spacer(minLength: Space.s2)
                        if let since = row.since {
                            TimelineView(.periodic(from: .now, by: 1)) { context in
                                Text(span(max(0, context.date.timeIntervalSince(since))))
                                    .font(TypeScale.meta.monospacedDigit())
                                    .foregroundStyle(Palette.text3)
                            }
                        }
                    }
                    .accessibilityElement(children: .combine)
                }
                if rows.more > 0 {
                    Text("+\(rows.more) more working").font(TypeScale.caption).foregroundStyle(Palette.text3)
                        .padding(.leading, 18)
                }
            }
            .font(TypeScale.callout)
            .padding(.vertical, Space.s1)
            .textSelection(.enabled)
        }
    }
}

/// Three dots pulsing in turn while something works; still under Reduce Motion.
struct WorkingMark: View {
    var size: CGFloat = 5
    @Environment(\.accessibilityReduceMotion) private var still

    var body: some View {
        Group {
            if still {
                dots { _ in 1 }
            } else {
                TimelineView(.animation(minimumInterval: 1 / 20)) { context in
                    let t = context.date.timeIntervalSinceReferenceDate
                    dots { i in 0.3 + 0.7 * (0.5 + 0.5 * sin((t * 2 * .pi / 1.2) - Double(i) * 0.9)) }
                }
            }
        }
        .accessibilityLabel("Working")
        .help("Working")
    }

    private func dots(_ opacity: @escaping (Int) -> Double) -> some View {
        HStack(spacing: size * 0.6) {
            ForEach(0..<3, id: \.self) { i in
                Circle().fill(Palette.accent).frame(width: size, height: size).opacity(opacity(i))
            }
        }
    }
}

/// Words with a light passing over them while work goes on; plain under
/// Reduce Motion.
struct Shimmer: View {
    let text: String
    @Environment(\.accessibilityReduceMotion) private var still

    var body: some View {
        let base = Text(text).foregroundStyle(Palette.text2)
        if still {
            base
        } else {
            base.overlay {
                TimelineView(.animation(minimumInterval: 1 / 30)) { context in
                    let phase = context.date.timeIntervalSinceReferenceDate.truncatingRemainder(dividingBy: 2) / 2
                    LinearGradient(stops: [
                        .init(color: .clear, location: phase - 0.25),
                        .init(color: Palette.page.opacity(0.75), location: phase),
                        .init(color: .clear, location: phase + 0.25),
                    ], startPoint: .leading, endPoint: .trailing)
                }
                .mask(base)
                .allowsHitTesting(false)
            }
        }
    }
}
