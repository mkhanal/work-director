import Foundation

/// A file the person is about to send, waiting above the field.
public struct Attaching: Sendable, Hashable, Identifiable {
    public let path: String
    public let name: String
    public let size: Int

    public var id: String { path }
    public var isImage: Bool { Attaching.imageKinds.contains((name as NSString).pathExtension.lowercased()) }

    static let imageKinds: Set<String> = ["png", "jpg", "jpeg", "gif", "heic", "webp", "tiff", "tif", "bmp"]

    /// A file on disk, as it is now. A folder is refused: its files are attached instead.
    public static func file(_ url: URL) throws -> Attaching {
        let values = try url.resourceValues(forKeys: [.isDirectoryKey, .fileSizeKey])
        guard values.isDirectory != true else { throw AttachError.folder(url.lastPathComponent) }
        return Attaching(path: url.path, name: url.lastPathComponent, size: values.fileSize ?? 0)
    }

    /// A pasted image, written as a PNG of its own in dir.
    public static func png(_ data: Data, in dir: URL, now: Date = Date()) throws -> Attaching {
        try FileManager.default.createDirectory(at: dir, withIntermediateDirectories: true)
        let stamp = ISO8601DateFormatter().string(from: now).replacingOccurrences(of: ":", with: "-")
        let url = dir.appendingPathComponent("pasted-\(stamp)-\(UUID().uuidString.prefix(4)).png")
        try data.write(to: url)
        return try file(url)
    }

    /// Where pasted images are written before they are sent.
    public static var pasteDir: URL {
        FileManager.default.temporaryDirectory.appendingPathComponent("work-director-pasted")
    }
}

public enum AttachError: Error, CustomStringConvertible, Equatable {
    case folder(String)

    public var description: String {
        switch self {
        case .folder(let name): "\(name) is a folder; attach the files in it"
        }
    }
}

/// A file attached under a goal, as the goal view serves it.
public struct Attachment: Codable, Sendable, Hashable, Identifiable {
    public let event: Int
    public let work: String
    public let at: String
    public let name: String
    public let path: String
    public let size: Int
    public let mediaType: String

    public var id: Int { event }
    public var isImage: Bool { mediaType.hasPrefix("image/") }

    enum CodingKeys: String, CodingKey {
        case event, work, at, name, path, size
        case mediaType = "media_type"
    }
}

extension Plan {
    /// The plan with each file passed as `--attach <path>`.
    public func attaching(_ files: [Attaching]) -> Plan {
        Plan(title: title, argv: argv + files.flatMap { ["--attach", $0.path] })
    }

    /// The paths it attaches, in order.
    public var attached: [String] {
        zip(argv, argv.dropFirst()).filter { $0.0 == "--attach" }.map(\.1)
    }
}

extension GoalDetail {
    /// Every file attached under the goal, oldest first.
    public var files: [Attachment] { attachments ?? [] }

    /// The files sent with a message: those on its work at its moment.
    public func files(with e: Event) -> [Attachment] {
        files.filter { $0.work == e.work && $0.at == e.at }
    }

    /// Whether an attachment event shows on its own: no message of the
    /// person's carries it, and no earlier file sent at its moment shows it.
    func showsAlone(_ e: Event) -> Bool {
        let carried = events.contains { m in m.id != e.id && m.work == e.work && m.at == e.at && (m.kind == "decision" || m.kind == "answer") }
        let first = events.first { $0.kind == "attachment" && $0.work == e.work && $0.at == e.at }
        return !carried && first?.id == e.id
    }
}

/// A size as a person reads it: 900 bytes, 12 KB, 3.4 MB.
public func fileSize(_ n: Int) -> String {
    ByteCountFormatter.string(fromByteCount: Int64(n), countStyle: .file)
}
