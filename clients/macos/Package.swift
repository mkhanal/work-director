// swift-tools-version: 6.0
import PackageDescription

let package = Package(
    name: "WorkDirector",
    platforms: [.macOS(.v15)],
    products: [
        .executable(name: "WorkDirector", targets: ["WorkDirector"]),
    ],
    targets: [
        .target(name: "WorkDirectorKit"),
        .executableTarget(name: "WorkDirector", dependencies: ["WorkDirectorKit"]),
        .testTarget(name: "WorkDirectorKitTests", dependencies: ["WorkDirectorKit"]),
    ]
)
