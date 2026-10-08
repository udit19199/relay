import SwitcherCore
import Combine
import Foundation

@MainActor
final class AppModel: AccountController, @MainActor ObservableObject {
    let objectWillChange = ObservableObjectPublisher()

    override init(store: AccountStore, codex: any AccountClient, switchService: any SwitchServicing) {
        super.init(store: store, codex: codex, switchService: switchService)
        onChange = { [weak self] in self?.objectWillChange.send() }
    }

    static func live() -> AppModel {
        let store = AccountStore()
        let codex = CodexClient()
        return AppModel(store: store, codex: codex,
                        switchService: SwitchService(desktop: DesktopController(), store: store, codex: codex))
    }
}
