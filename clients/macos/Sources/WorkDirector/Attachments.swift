import AppKit
import QuickLookUI
import SwiftUI
import WorkDirectorKit

/// The files waiting to go with a message, each with a way to take it off.
struct AttachingChips: View {
    @Binding var files: [Attaching]

    var body: some View {
        ScrollView(.horizontal, showsIndicators: false) {
            HStack(spacing: Space.s2) {
                ForEach(files) { file in
                    FileChip(name: file.name, size: file.size, path: file.path, image: file.isImage) {
                        files.removeAll { $0 == file }
                    }
                }
            }
        }
    }
}

/// The files sent with a message, under it: images as thumbnails that open
/// in Quick Look, other files as chips that show in Finder.
struct SentFiles: View {
    let files: [Attachment]

    var body: some View {
        FlowLayout(spacing: Space.s2) {
            ForEach(files) { file in
                Button { open(file) } label: {
                    FileChip(name: file.name, size: file.size, path: file.path, image: file.isImage)
                }
                .buttonStyle(.plain)
                .help(file.isImage ? "Open \(file.name) in Quick Look" : "Show \(file.name) in Finder")
                .contextMenu {
                    Button("Open") { NSWorkspace.shared.open(URL(fileURLWithPath: file.path)) }
                    Button("Show in Finder") { NSWorkspace.shared.activateFileViewerSelecting([URL(fileURLWithPath: file.path)]) }
                    Button("Copy Path") { copyText(file.path) }
                }
            }
        }
    }

    private func open(_ file: Attachment) {
        let url = URL(fileURLWithPath: file.path)
        if file.isImage { QuickLook.shared.show(url) } else { NSWorkspace.shared.activateFileViewerSelecting([url]) }
    }
}

/// One file: a thumbnail for an image, else its icon, with its name and size.
struct FileChip: View {
    let name: String
    let size: Int
    let path: String
    let image: Bool
    var remove: (() -> Void)? = nil

    var body: some View {
        HStack(spacing: Space.s2) {
            if image, let thumb = NSImage(contentsOfFile: path) {
                Image(nsImage: thumb).resizable().aspectRatio(contentMode: .fill)
                    .frame(width: 36, height: 36)
                    .clipShape(RoundedRectangle(cornerRadius: Radius.chip))
            } else {
                Image(nsImage: NSWorkspace.shared.icon(forFile: path)).resizable().frame(width: 28, height: 28)
            }
            VStack(alignment: .leading, spacing: 1) {
                Text(name).font(TypeScale.meta).foregroundStyle(Palette.text).lineLimit(1).truncationMode(.middle)
                Text(fileSize(size)).font(TypeScale.small).foregroundStyle(Palette.text3)
            }
            .frame(maxWidth: 180, alignment: .leading)
            if let remove {
                Button(action: remove) { Image(systemName: "xmark.circle.fill").foregroundStyle(Palette.text3) }
                    .buttonStyle(.plain)
                    .help("Remove \(name)")
                    .accessibilityLabel("Remove \(name)")
            }
        }
        .padding(Space.s1 + 2)
        .background(Palette.sunken, in: RoundedRectangle(cornerRadius: Radius.card))
        .accessibilityElement(children: .contain)
    }
}

/// The Quick Look panel, showing one file at a time.
@MainActor
final class QuickLook: NSObject, QLPreviewPanelDataSource {
    static let shared = QuickLook()
    private var url: URL?

    func show(_ url: URL) {
        self.url = url
        guard let panel = QLPreviewPanel.shared() else { return }
        panel.dataSource = self
        panel.reloadData()
        panel.makeKeyAndOrderFront(nil)
    }

    nonisolated func numberOfPreviewItems(in panel: QLPreviewPanel!) -> Int { 1 }

    nonisolated func previewPanel(_ panel: QLPreviewPanel!, previewItemAt index: Int) -> (any QLPreviewItem)! {
        MainActor.assumeIsolated { url as NSURL? }
    }
}
