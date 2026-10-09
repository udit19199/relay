import SidekickCore
import AppKit

@main
@MainActor
enum SidekickApp {
    static func main() {
        let application = NSApplication.shared
        let delegate = SidekickAppDelegate()
        application.delegate = delegate
        application.setActivationPolicy(.accessory)
        application.run()
    }
}

@MainActor
final class SidekickAppDelegate: NSObject, NSApplicationDelegate {
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
