// SPDX-License-Identifier: MIT
import RelayCore
import AppKit

@main
@MainActor
enum RelayApp {
    static func main() {
        let application = NSApplication.shared
        let delegate = RelayAppDelegate()
        application.delegate = delegate
        application.setActivationPolicy(.accessory)
        application.run()
    }
}

@MainActor
final class RelayAppDelegate: NSObject, NSApplicationDelegate {
    private var model: AppModel?
    private var islandPanelController: DynamicIslandPanelController?

    func applicationDidFinishLaunching(_ notification: Notification) {
        let model = AppModel.live()
        let controller = DynamicIslandPanelController(model: model)
        self.model = model
        islandPanelController = controller
        controller.show()
        Task { await model.startBackgroundUsageRefresh() }
    }
}
