// SPDX-License-Identifier: MIT
import SidekickCore
import Combine
import Foundation

@MainActor
final class AppModel: AccountController, @MainActor ObservableObject {
    let objectWillChange = ObservableObjectPublisher()

    override init(client: RelayClient) {
        super.init(client: client)
        onChange = { [weak self] in self?.objectWillChange.send() }
    }

    static func live() -> AppModel {
        let client: RelayClient
        do {
            client = try RelayClient.live()
        } catch {
            // Controller surfaces the key error in the island; refreshes retry it.
            client = RelayClient(
                baseURL: URL(string: "http://127.0.0.1:8317")!,
                managementKey: ""
            )
        }
        return AppModel(client: client)
    }
}
