// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Vorssaint for the adapted panel configuration.
// Portions adapted from Vorssaint's NotchWindowHost.swift at
// https://github.com/vorssaint/vorssaint-utils/commit/cb84ea01410710350dc373417715990351f79f00.

import AppKit
import Combine
import SidekickCore
import SwiftUI

@MainActor
final class DynamicIslandPanelController: NSObject, ObservableObject {
    private let overlaySpace = NotchOverlaySpace()
    private let panel: NotchPanel
    private let model: AppModel
    private var hoverTask: Task<Void, Never>?
    private var outsideClickMonitors: [Any] = []
    private let menuSpaceQueue = DispatchQueue(label: "com.sidekick.notch.menu-space", qos: .utility)
    private var menuSpaceTimer: Timer?
    private var menuSpaceGeneration = 0
    @Published private(set) var isExpanded = false
    @Published private(set) var hasActiveConfirmation = false
    @Published fileprivate private(set) var layout: NotchGeometry

    init(model: AppModel) {
        let layout = NotchGeometry.current() ?? NotchGeometry(
            screen: NSScreen.main?.frame ?? CGRect(x: 0, y: 0, width: 1440, height: 900),
            safeAreaTop: 0,
            cameraWidth: 0
        )
        self.model = model
        self.layout = layout
        panel = NotchPanel(
            contentRect: layout.frame(for: layout.expandedSize(titleWidth: 0)),
            styleMask: [.borderless, .nonactivatingPanel],
            backing: .buffered,
            defer: false
        )
        super.init()

        panel.isFloatingPanel = true
        panel.level = NotchPanel.normalLevel
        panel.backgroundColor = .clear
        panel.isOpaque = false
        panel.hasShadow = false
        panel.hidesOnDeactivate = false
        panel.becomesKeyOnlyIfNeeded = true
        panel.isReleasedWhenClosed = false
        panel.collectionBehavior = NotchPanel.overlayCollectionBehavior
        panel.animationBehavior = .none
        let hostingView = IslandHostingView(rootView: DynamicIslandPanel(model: model, controller: self))
        hostingView.controller = self
        panel.contentView = hostingView
        updateFrame()
        overlaySpace?.add(panel)
        installOutsideClickMonitors()

        NotificationCenter.default.addObserver(
            self,
            selector: #selector(screenParametersDidChange(_:)),
            name: NSApplication.didChangeScreenParametersNotification,
            object: nil
        )
        NSWorkspace.shared.notificationCenter.addObserver(
            self,
            selector: #selector(frontmostApplicationDidChange(_:)),
            name: NSWorkspace.didActivateApplicationNotification,
            object: nil
        )
        menuSpaceTimer = Timer.scheduledTimer(withTimeInterval: 30, repeats: true) { [weak self] _ in
            Task { @MainActor in self?.measureMenuBarSpace() }
        }
    }

    isolated deinit {
        panel.orderOut(nil)
        overlaySpace?.close()
        hoverTask?.cancel()
        menuSpaceTimer?.invalidate()
        outsideClickMonitors.forEach(NSEvent.removeMonitor)
        NotificationCenter.default.removeObserver(self)
        NSWorkspace.shared.notificationCenter.removeObserver(self)
    }

    func show() {
        updateFrame()
        panel.orderFrontRegardless()
        updateMousePassthrough(at: NSEvent.mouseLocation)
        measureMenuBarSpace()
    }

    func toggleExpansion() {
        hoverTask?.cancel()
        setExpanded(!isExpanded, makeKey: !isExpanded)
    }

    func collapse() {
        hoverTask?.cancel()
        setExpanded(false)
    }

    fileprivate func pointerEnteredIsland() {
        hoverTask?.cancel()
        hoverTask = Task { @MainActor [weak self] in
            try? await Task.sleep(for: .milliseconds(140))
            guard !Task.isCancelled else { return }
            self?.setExpanded(true)
        }
    }

    fileprivate func pointerLeftIsland() {
        hoverTask?.cancel()
        guard isExpanded, !hasActiveConfirmation else { return }
        hoverTask = Task { @MainActor [weak self] in
            try? await Task.sleep(for: .milliseconds(260))
            guard !Task.isCancelled, let self, NSApp.modalWindow == nil else { return }
            self.setExpanded(false)
        }
    }

    private func setExpanded(_ expanded: Bool, makeKey: Bool = false) {
        guard expanded != isExpanded else { return }
        isExpanded = expanded
        updateMousePassthrough(at: NSEvent.mouseLocation)
        if let contentView = panel.contentView { panel.invalidateCursorRects(for: contentView) }
        if expanded {
            if makeKey { panel.makeKey() }
        } else if panel.isKeyWindow {
            panel.resignKey()
        }
    }

    fileprivate func setConfirmationActive(_ active: Bool) {
        hasActiveConfirmation = active
    }

    fileprivate func updateFrame() {
        panel.setFrame(layout.frame(for: panelSize), display: true, animate: false)
        updateMousePassthrough(at: NSEvent.mouseLocation)
    }

    @objc private func screenParametersDidChange(_ notification: Notification) {
        layout = currentGeometry()
        updateFrame()
        measureMenuBarSpace()
    }

    @objc private func frontmostApplicationDidChange(_ notification: Notification) {
        measureMenuBarSpace()
    }

    fileprivate var panelSize: CGSize {
        let chromeHeight = layout.safeContentTop + NotchLayout.bottomInset
        let accountRows = CGFloat(max(1, model.accounts.count))
        let errorAllowance: CGFloat = model.visibleError == nil ? 0 : 60
        let desiredHeight = chromeHeight + 63 + accountRows * 104 + errorAllowance
        return CGSize(width: layout.expandedWidth, height: min(min(desiredHeight, 440), layout.screen.height - 48))
    }
    fileprivate var visibleHeight: CGFloat { isExpanded ? panelSize.height : layout.collapsed.height }
    fileprivate var visibleSurfaceWidth: CGFloat { isExpanded ? panelSize.width : layout.collapsed.width }
    private func currentGeometry() -> NotchGeometry {
        NotchGeometry.current() ?? NotchGeometry(
            screen: NSScreen.main?.frame ?? layout.screen,
            safeAreaTop: 0,
            cameraWidth: 0
        )
    }

    private func installOutsideClickMonitors() {
        let outsideClick: (NSEvent) -> Void = { [weak self] _ in
            Task { @MainActor in
                guard let self, self.isExpanded, !self.hasActiveConfirmation, NSApp.modalWindow == nil else { return }
                let point = NSEvent.mouseLocation
                let visibleFrame = self.layout.frame(for: CGSize(width: self.panelSize.width, height: self.visibleHeight))
                if !visibleFrame.contains(point) { self.collapse() }
            }
        }
        if let monitor = NSEvent.addGlobalMonitorForEvents(matching: .leftMouseDown, handler: outsideClick) {
            outsideClickMonitors.append(monitor)
        }
        if let monitor = NSEvent.addLocalMonitorForEvents(matching: .leftMouseDown, handler: { event in
            outsideClick(event)
            return event
        }) {
            outsideClickMonitors.append(monitor)
        }
        if let monitor = NSEvent.addLocalMonitorForEvents(matching: .keyDown, handler: { [weak self] event in
            guard event.keyCode == 53 else { return event }
            let keepForDialog = MainActor.assumeIsolated {
                self?.hasActiveConfirmation == true || NSApp.modalWindow != nil
            }
            guard !keepForDialog else { return event }
            Task { @MainActor in self?.collapse() }
            return nil
        }) {
            outsideClickMonitors.append(monitor)
        }
        let updatePassthrough: (NSEvent) -> Void = { [weak self] _ in
            Task { @MainActor in self?.updateMousePassthrough(at: NSEvent.mouseLocation) }
        }
        if let monitor = NSEvent.addGlobalMonitorForEvents(matching: .mouseMoved, handler: updatePassthrough) {
            outsideClickMonitors.append(monitor)
        }
        if let monitor = NSEvent.addLocalMonitorForEvents(matching: .mouseMoved, handler: { event in
            updatePassthrough(event)
            return event
        }) {
            outsideClickMonitors.append(monitor)
        }
    }

    private func updateMousePassthrough(at point: CGPoint) {
        let frame = layout.frame(for: CGSize(width: visibleSurfaceWidth, height: visibleHeight))
        panel.ignoresMouseEvents = !frame.contains(point)
    }

    private func measureMenuBarSpace() {
        guard layout.isNotched else { return }
        menuSpaceGeneration += 1
        let generation = menuSpaceGeneration
        let geometry = layout
        let primaryTop = NSScreen.screens.first?.frame.maxY ?? geometry.screen.maxY
        let ownWindow = panel.windowNumber
        let pid = NSWorkspace.shared.frontmostApplication?.processIdentifier ?? getpid()
        menuSpaceQueue.async { [weak self] in
            let room = NotchMenuBarSpace.measure(
                pid: pid,
                geometry: geometry,
                primaryTop: primaryTop,
                ownWindow: ownWindow
            )
            DispatchQueue.main.async {
                guard let self, self.menuSpaceGeneration == generation else { return }
                var measuredGeometry = geometry
                measuredGeometry.compactSideRoom = room
                self.layout = measuredGeometry
                self.updateFrame()
            }
        }
    }
}

private final class NotchPanel: NSPanel {
    override var canBecomeKey: Bool { true }
    override var canBecomeMain: Bool { false }

    static let overlayCollectionBehavior: NSWindow.CollectionBehavior = [
        .canJoinAllSpaces, .fullScreenAuxiliary, .stationary, .ignoresCycle
    ]
    static let normalLevel = NSWindow.Level(rawValue: NSWindow.Level.statusBar.rawValue + 1)
}

private final class IslandHostingView<Content: View>: NSHostingView<Content> {
    weak var controller: DynamicIslandPanelController?

    override func resetCursorRects() {
        super.resetCursorRects()
        addCursorRect(visibleSurfaceRect, cursor: .arrow)
    }

    private var visibleSurfaceRect: CGRect {
        guard let controller else { return .zero }
        return CGRect(
            x: bounds.midX - controller.visibleSurfaceWidth / 2,
            y: isFlipped ? bounds.minY : bounds.maxY - controller.visibleHeight,
            width: controller.visibleSurfaceWidth,
            height: controller.visibleHeight
        )
    }

    override func hitTest(_ point: NSPoint) -> NSView? {
        guard controller != nil else { return nil }
        guard visibleSurfaceRect.contains(point) else { return nil }
        return super.hitTest(point)
    }
}

private struct DynamicIslandPanel: View {
    @ObservedObject var model: AppModel
    @ObservedObject var controller: DynamicIslandPanelController
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var isHovered = false

    private var activeAccount: AccountProfile? {
        model.accounts.first { $0.id == model.activeAccountID }
    }

    private var activeAccountName: String {
        guard let activeAccount else {
            return model.accounts.isEmpty ? "Add an account" : "No active account"
        }
        return activeAccount.email.flatMap { $0.isEmpty ? nil : $0 } ?? activeAccount.displayName
    }

    private var islandShape: NotchShape {
        NotchShape.island(height: controller.visibleHeight, geometry: controller.layout)
    }

    var body: some View {
        VStack(spacing: 0) {
            if controller.isExpanded {
                IslandAccountManager(
                    model: model,
                    onDismiss: controller.collapse,
                    onConfirmationChanged: { isActive in
                        controller.setConfirmationActive(isActive)
                        if !isActive && !isHovered { controller.pointerLeftIsland() }
                    }
                )
                    .padding(.horizontal, NotchLayout.horizontalInset)
                    .padding(.top, controller.layout.safeContentTop)
                    .padding(.bottom, NotchLayout.bottomInset)
                    .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .top)
                    .transition(.identity)
            } else {
                compactHeader
            }
        }
        .frame(width: controller.visibleSurfaceWidth, height: controller.visibleHeight, alignment: .top)
        .background { islandShape.fill(Color.black) }
        .clipShape(islandShape)
        .preferredColorScheme(.dark)
        .animation(expansionAnimation, value: controller.isExpanded)
        .animation(.easeOut(duration: 0.15), value: isHovered)
        .onHover { hovering in
            isHovered = hovering
            if hovering {
                NSCursor.arrow.set()
                controller.pointerEnteredIsland()
            }
            else { controller.pointerLeftIsland() }
        }
        .onExitCommand(perform: controller.collapse)
        .frame(width: controller.panelSize.width, height: controller.panelSize.height, alignment: .top)
        .onChange(of: model.accounts.count) { _, _ in controller.updateFrame() }
        .onChange(of: model.visibleError != nil) { _, _ in controller.updateFrame() }
    }

    private var expansionAnimation: Animation {
        reduceMotion ? .easeOut(duration: 0.12) : .spring(response: 0.42, dampingFraction: 0.9)
    }

    private var compactHeader: some View {
        Button(action: controller.toggleExpansion) {
            Group {
                if controller.layout.isNotched {
                    HStack(spacing: 0) {
                        if controller.layout.restingWingWidth > 0 {
                            compactFiveHourValue
                            .frame(width: controller.layout.restingWingWidth)
                        }
                        Color.clear.frame(width: controller.layout.cameraWidth)
                        if controller.layout.restingWingWidth > 0 {
                            compactWeeklyValue.frame(width: controller.layout.restingWingWidth)
                        }
                    }
                } else {
                    HStack(spacing: 8) {
                        compactFiveHourValue
                        Spacer(minLength: 4)
                        compactWeeklyValue
                        Image(systemName: "chevron.down")
                            .font(.system(size: 6, weight: .bold))
                            .foregroundStyle(.white.opacity(0.68))
                    }
                    .padding(.horizontal, 13)
                }
            }
            .frame(height: controller.layout.collapsed.height)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel(controller.isExpanded ? "Collapse Sidekick" : "Open Sidekick")
        .accessibilityValue("\(activeAccountName), weekly usage \(model.activeRemainingPercent.map { "\($0)" } ?? "unavailable") percent remaining")
        .help("\(activeAccountName) · Show account details")
    }

    private var compactFiveHourValue: some View {
        compactUsageValue("5H", percent: model.activeAccountID.flatMap {
            model.usageStates[$0]?.displayedUsage?.fiveHourRemainingPercent
        })
    }

    private var compactWeeklyValue: some View {
        compactUsageValue("WK", percent: model.activeRemainingPercent)
    }

    private func compactUsageValue(_ label: String, percent: Int?) -> some View {
        VStack(spacing: 1) {
            Text(label)
                .font(.system(size: 8, weight: .medium))
                .foregroundStyle(.white.opacity(0.65))
            Text(percent.map { "\($0)%" } ?? "—")
                .font(.system(size: 11, weight: .semibold).monospacedDigit())
                .foregroundStyle(UsageTint.color(for: percent))
                .lineLimit(1)
                .minimumScaleFactor(0.8)
        }
        .frame(width: 44, height: controller.layout.collapsed.height)
        .help("\(label == "5H" ? "5-hour" : "Weekly") usage · \(percent.map { "\($0)% remaining" } ?? "Unavailable")")
    }

}
