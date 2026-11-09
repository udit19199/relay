// swift-tools-version: 6.4
// SPDX-License-Identifier: MIT

import PackageDescription

let package = Package(
    name: "Relay",
    platforms: [
        .macOS(.v14),
    ],
    products: [
        .executable(name: "Relay", targets: ["Relay"]),
    ],
    targets: [
        .target(name: "RelayCore"),
        .executableTarget(
            name: "Relay",
            dependencies: ["RelayCore"],
            linkerSettings: [
                .linkedFramework("AppKit"),
            ]
        ),
    ]
)
