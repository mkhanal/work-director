import Foundation

public enum LocatorError: Error, Equatable, CustomStringConvertible {
    case notInBundle(String)
    case noWD

    public var description: String {
        switch self {
        case .notInBundle(let bundle):
            "no wd inside \(bundle); rebuild the app with scripts/build-macos.sh"
        case .noWD:
            "no wd to run: not inside an app bundle, and WD_BIN is not set"
        }
    }
}

/// Locate names the wd this app runs. A wd found on PATH could be any version,
/// and the app reads exactly the wire of the wd it was built with.
public func locateWD(bundle: Bundle = .main, environment: [String: String] = ProcessInfo.processInfo.environment) throws -> URL {
    if bundle.bundleURL.pathExtension == "app" {
        guard let wd = bundle.url(forAuxiliaryExecutable: "wd"),
              FileManager.default.isExecutableFile(atPath: wd.path)
        else { throw LocatorError.notInBundle(bundle.bundleURL.path) }
        return wd
    }
    guard let path = environment["WD_BIN"], !path.isEmpty else { throw LocatorError.noWD }
    return URL(fileURLWithPath: path)
}
