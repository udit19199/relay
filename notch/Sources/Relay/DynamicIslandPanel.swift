// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Vorssaint for the adapted panel configuration.
// Portions adapted from Vorssaint's NotchWindowHost.swift at
// https://github.com/vorssaint/vorssaint-utils/commit/cb84ea01410710350dc373417715990351f79f00.

import AppKit
import Combine
import QuartzCore
import RelayCore
import SwiftUI

@MainActor
final class DynamicIslandPanelController: NSObject, ObservableObject {
    private let overlaySpace = NotchOverlaySpace()
    private let panel: NotchPanel
    private let model: AppModel
    private var hoverState = IslandHoverState()
    private var pointerTimer: Timer?
    private var outsideClickMonitors: [Any] = []
    private let menuSpaceQueue = DispatchQueue(
        label: "com.udit19199.relay.notch.menu-space", qos: .utility)
    private var menuSpaceTimer: Timer?
    private var menuSpaceGeneration = 0
    private var frameAnimationGeneration = 0
    private var isSurfaceAnimating = false
    @Published private(set) var isExpanded = false
    @Published var showsCompactUsage =
        UserDefaults.standard.object(forKey: "showsCompactUsage") as? Bool ?? false
    {
        didSet {
            UserDefaults.standard.set(showsCompactUsage, forKey: "showsCompactUsage")
        }
    }
    @Published private(set) var hasActiveConfirmation = false
    @Published fileprivate private(set) var layout: NotchGeometry
    @Published fileprivate var expandedBodyLayout = IslandExpandedBodyLayout.accounts(contentHeight: 300)
    @Published private(set) var expandedPanelHeight: CGFloat = 0

    init(model: AppModel) {
        let layout =
            NotchGeometry.current()
            ?? NotchGeometry(
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

        panel.acceptsMouseMovedEvents = true
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
        let hostingView = IslandHostingView(
            rootView: DynamicIslandPanel(model: model, controller: self))
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
        menuSpaceTimer = Timer.scheduledTimer(withTimeInterval: 30, repeats: true) {
            [weak self] _ in
            Task { @MainActor in self?.measureMenuBarSpace() }
        }
    }

    isolated deinit {
        panel.orderOut(nil)
        overlaySpace?.close()
        pointerTimer?.invalidate()
        menuSpaceTimer?.invalidate()
        outsideClickMonitors.forEach(NSEvent.removeMonitor)
        NotificationCenter.default.removeObserver(self)
        NSWorkspace.shared.notificationCenter.removeObserver(self)
    }

    func show() {
        updateFrame()
        panel.orderFrontRegardless()
        reconcilePointer()
        if pointerTimer == nil {
            // Read the position even if an entry event is missed or the pointer stops moving.
            let timer = Timer(timeInterval: 0.05, repeats: true) { [weak self] _ in
                MainActor.assumeIsolated { self?.reconcilePointer() }
            }
            RunLoop.main.add(timer, forMode: .common)
            pointerTimer = timer
        }
        measureMenuBarSpace()
    }

    func toggleExpansion() {
        if isExpanded { collapse() } else { setExpanded(true, makeKey: true) }
    }

    func collapse() {
        hoverState.dismiss()
        setExpanded(false)
    }

    /// Drops expanded UI immediately and snaps the panel back to the compact strip.
    private func snapToCompact() {
        frameAnimationGeneration += 1
        isSurfaceAnimating = false
        isExpanded = false
        updateFrame(animated: false)
        refreshPointerArtifacts()
    }

    private func reconcilePointer() {
        let point = NSEvent.mouseLocation
        let inside = IslandHoverState.contains(point, in: panel.frame)
        panel.ignoresMouseEvents = !inside
        if let expanded = hoverState.update(
            inside: inside,
            expanded: isExpanded,
            keepOpen: hasActiveConfirmation || NSApp.modalWindow != nil,
            now: ProcessInfo.processInfo.systemUptime
        ) {
            if expanded && isSurfaceAnimating { return }
            setExpanded(expanded)
        }
    }

    private func setExpanded(_ expanded: Bool, makeKey: Bool = false) {
        if expanded {
            guard !isExpanded else { return }
            isExpanded = true
            updateFrame(
                animated: !IslandMotion.prefersReducedMotion,
                duration: IslandMotion.expandDuration,
                timing: IslandMotion.expandTiming()
            )
            if makeKey { panel.makeKey() }
            refreshPointerArtifacts()
            return
        }

        guard isExpanded else { return }
        if panel.isKeyWindow { panel.resignKey() }
        isExpanded = false
        updateFrame(
            animated: !IslandMotion.prefersReducedMotion,
            duration: IslandMotion.collapseDuration,
            timing: IslandMotion.collapseTiming()
        )
        refreshPointerArtifacts()
    }

    private func refreshPointerArtifacts() {
        panel.ignoresMouseEvents = !IslandHoverState.contains(NSEvent.mouseLocation, in: panel.frame)
        if let contentView = panel.contentView {
            contentView.updateTrackingAreas()
            panel.invalidateCursorRects(for: contentView)
        }
    }

    fileprivate func setConfirmationActive(_ active: Bool) {
        hasActiveConfirmation = active
        reconcilePointer()
    }

    fileprivate func setExpandedBodyLayout(_ layout: IslandExpandedBodyLayout) {
        guard expandedBodyLayout != layout else { return }
        expandedBodyLayout = layout
        guard isExpanded else {
            updateFrame(animated: false)
            return
        }
        updateFrame(
            animated: !IslandMotion.prefersReducedMotion,
            duration: IslandMotion.morphDuration,
            timing: IslandMotion.morphTiming()
        )
    }

    fileprivate func updateFrame(
        animated: Bool = false,
        duration: TimeInterval = IslandMotion.morphDuration,
        timing: CAMediaTimingFunction = IslandMotion.morphTiming()
    ) {
        let size = targetSurfaceSize
        let newFrame = layout.frame(for: size)
        expandedPanelHeight = size.height

        guard animated else {
            isSurfaceAnimating = false
            panel.setFrame(newFrame, display: true, animate: false)
            finalizeFrameLayout()
            return
        }

        frameAnimationGeneration += 1
        let generation = frameAnimationGeneration
        isSurfaceAnimating = true
        NSAnimationContext.runAnimationGroup { context in
            context.duration = duration
            context.timingFunction = timing
            context.allowsImplicitAnimation = true
            panel.animator().setFrame(newFrame, display: true)
        } completionHandler: { [weak self] in
            Task { @MainActor in
                guard let self, self.frameAnimationGeneration == generation else { return }
                self.isSurfaceAnimating = false
                self.finalizeFrameLayout()
            }
        }
    }

    private func finalizeFrameLayout() {
        panel.contentView?.needsLayout = true
        panel.contentView?.layoutSubtreeIfNeeded()
        panel.contentView?.updateTrackingAreas()
        if let contentView = panel.contentView {
            panel.invalidateCursorRects(for: contentView)
        }
        reconcilePointer()
    }

    @objc private func screenParametersDidChange(_ notification: Notification) {
        layout = currentGeometry()
        updateFrame()
        measureMenuBarSpace()
    }

    @objc private func frontmostApplicationDidChange(_ notification: Notification) {
        measureMenuBarSpace()
    }

    fileprivate var targetSurfaceSize: CGSize {
        isExpanded ? panelSize : layout.collapsed
    }

    fileprivate var panelSize: CGSize {
        let chromeHeight = layout.safeContentTop + NotchLayout.bottomInset
        // The accounts body is measured and already includes the error banner.
        let errorAllowance: CGFloat =
            if case .addContent = expandedBodyLayout, model.visibleError != nil { 60 } else { 0 }
        let desiredHeight = chromeHeight + expandedBodyLayout.bodyHeight + errorAllowance
        // Past this the account list scrolls instead of growing.
        let maxHeight = min(layout.screen.height - 48, 640)
        return CGSize(
            width: expandedBodyLayout.panelWidth(expandedWidth: layout.expandedWidth),
            height: min(desiredHeight, maxHeight))
    }
    fileprivate var visibleHeight: CGFloat { panel.frame.height }
    fileprivate var visibleSurfaceWidth: CGFloat { panel.frame.width }
    private func currentGeometry() -> NotchGeometry {
        NotchGeometry.current()
            ?? NotchGeometry(
                screen: NSScreen.main?.frame ?? layout.screen,
                safeAreaTop: 0,
                cameraWidth: 0
            )
    }

    private func installOutsideClickMonitors() {
        let outsideClick: (NSEvent) -> Void = { [weak self] _ in
            Task { @MainActor in
                guard let self, self.isExpanded, !self.hasActiveConfirmation,
                    NSApp.modalWindow == nil
                else { return }
                let point = NSEvent.mouseLocation
                if !IslandHoverState.contains(point, in: self.panel.frame) { self.collapse() }
            }
        }
        if let monitor = NSEvent.addGlobalMonitorForEvents(
            matching: .leftMouseDown, handler: outsideClick)
        {
            outsideClickMonitors.append(monitor)
        }
        if let monitor = NSEvent.addLocalMonitorForEvents(
            matching: .leftMouseDown,
            handler: { event in
                outsideClick(event)
                return event
            })
        {
            outsideClickMonitors.append(monitor)
        }
        if let monitor = NSEvent.addLocalMonitorForEvents(
            matching: .keyDown,
            handler: { [weak self] event in
                guard event.keyCode == 53 else { return event }
                let keepForDialog = MainActor.assumeIsolated {
                    self?.hasActiveConfirmation == true || NSApp.modalWindow != nil
                }
                guard !keepForDialog else { return event }
                Task { @MainActor in self?.collapse() }
                return nil
            })
        {
            outsideClickMonitors.append(monitor)
        }
        let updatePassthrough: (NSEvent) -> Void = { [weak self] _ in
            Task { @MainActor in self?.reconcilePointer() }
        }
        if let monitor = NSEvent.addGlobalMonitorForEvents(
            matching: [.mouseMoved, .leftMouseDragged, .rightMouseDragged, .otherMouseDragged],
            handler: updatePassthrough)
        {
            outsideClickMonitors.append(monitor)
        }
        if let monitor = NSEvent.addLocalMonitorForEvents(
            matching: [.mouseMoved, .leftMouseDragged, .rightMouseDragged, .otherMouseDragged],
            handler: { event in
                updatePassthrough(event)
                return event
            })
        {
            outsideClickMonitors.append(monitor)
        }
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
        .canJoinAllSpaces, .fullScreenAuxiliary, .stationary, .ignoresCycle,
    ]
    static let normalLevel = NSWindow.Level(rawValue: NSWindow.Level.statusBar.rawValue + 1)
}

private final class IslandHostingView<Content: View>: NSHostingView<Content> {
    weak var controller: DynamicIslandPanelController?
    override func resetCursorRects() {
        super.resetCursorRects()
        addCursorRect(visibleSurfaceRect, cursor: .arrow)
    }

    private var visibleSurfaceRect: CGRect { bounds }

    override func hitTest(_ point: NSPoint) -> NSView? {
        guard controller != nil else { return nil }
        guard visibleSurfaceRect.contains(point) else { return nil }
        return super.hitTest(point)
    }
}

private struct DynamicIslandPanel: View {
    @ObservedObject var model: AppModel
    @ObservedObject var controller: DynamicIslandPanelController
    @State private var isHovered = false

    private var headlineAccountName: String {
        switch model.accounts.count {
        case 0: "Add an account"
        case 1: model.accounts[0].email ?? model.accounts[0].displayName
        default: "All \(model.accounts.count) accounts"
        }
    }

    var body: some View {
        GeometryReader { geometry in
            let islandShape = NotchShape.island(height: geometry.size.height, geometry: controller.layout)
            ZStack(alignment: .top) {
                if controller.isExpanded {
                    IslandAccountManager(
                        model: model,
                        showsCompactUsage: $controller.showsCompactUsage,
                        onDismiss: controller.collapse,
                        onConfirmationChanged: { isActive in
                            controller.setConfirmationActive(isActive)
                        },
                        onExpandedBodyLayoutChanged: { layout in
                            controller.setExpandedBodyLayout(layout)
                        }
                    )
                    .padding(.horizontal, NotchLayout.horizontalInset)
                    .padding(.top, controller.layout.safeContentTop)
                    .padding(.bottom, NotchLayout.bottomInset)
                    // Lay out at the final size while the window grows or shrinks,
                    // so the island shape reveals the content instead of squeezing it.
                    .frame(
                        width: controller.panelSize.width,
                        height: controller.panelSize.height,
                        alignment: .top
                    )
                    .transition(.opacity)
                } else if controller.showsCompactUsage {
                    compactHeader
                        .transition(.opacity)
                } else {
                    Button(action: controller.toggleExpansion) {
                        Color.clear.contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("Open Relay")
                }
            }
            .frame(width: geometry.size.width, height: geometry.size.height, alignment: .top)
            .animation(
                IslandMotion.prefersReducedMotion ? nil : .easeInOut(duration: 0.18),
                value: controller.isExpanded
            )
            .background {
                if controller.isExpanded || controller.showsCompactUsage {
                    islandShape.fill(Color.black)
                }
            }
            .clipShape(islandShape)
        }
        .preferredColorScheme(.dark)
        .animation(.easeOut(duration: 0.15), value: isHovered)
        .onHover { hovering in
            isHovered = hovering
            if hovering {
                NSCursor.arrow.set()
            }
        }
        .onExitCommand(perform: controller.collapse)
        .onChange(of: model.accounts.count) { _, _ in controller.updateFrame() }
        .onChange(of: model.visibleError != nil) { _, _ in controller.updateFrame() }
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
        .accessibilityLabel(controller.isExpanded ? "Collapse Relay" : "Open Relay")
        .accessibilityValue(
            "\(headlineAccountName), weekly usage \(model.pooledUsage(at: .now).weekly.map { "\($0.remainingPercent)" } ?? "unavailable") percent remaining"
        )
        .help("\(headlineAccountName) · Show account details")
    }

    // Totals across all accounts; the island refreshes them every 30s.
    private var compactFiveHourValue: some View {
        compactUsageValue("5H", percent: model.pooledUsage(at: .now).fiveHour?.remainingPercent)
    }

    private var compactWeeklyValue: some View {
        compactUsageValue("WK", percent: model.pooledUsage(at: .now).weekly?.remainingPercent)
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
        .help(
            "\(label == "5H" ? "5-hour" : "Weekly") total across accounts · \(percent.map { "\($0)% remaining" } ?? "Unavailable")"
        )
    }

}
