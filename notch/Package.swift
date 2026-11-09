// swift-tools-version: 6.4
// SPDX-License-Identifier: MIT

import PackageDescription

let package = Package(
    name: "Sidekick",
    platforms: [
        .macOS(.v14),
    ],
    products: [
        .executable(name: "Sidekick", targets: ["Sidekick"]),
    ],
    targets: [
        .target(name: "SidekickCore"),
        .executableTarget(
            name: "Sidekick",
            dependencies: ["SidekickCore"],
            resources: [.process("Resources")],
            linkerSettings: [
                .linkedFramework("AppKit"),
            ]
        ),
    ]
)
