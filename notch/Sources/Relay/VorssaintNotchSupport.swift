// SPDX-License-Identifier: GPL-3.0-or-later
// Copyright (C) 2026 Vorssaint
// Adapted from Sources/Vorssaint/Services/Notch/NotchSupport.swift and
// Sources/Vorssaint/Services/Notch/NotchService.swift at
// https://github.com/vorssaint/vorssaint-utils/commit/cb84ea01410710350dc373417715990351f79f00.

import AppKit

enum NotchLayout {
    static let shoulder: CGFloat = 14
    static let horizontalInset: CGFloat = 28
    static let headerHeight: CGFloat = 36
    static let spacing: CGFloat = 12
    static let bottomInset: CGFloat = 16
    static let compactContentHeight: CGFloat = 180
    static let capsuleMargin: CGFloat = 2

    @MainActor
    static func headerTitleWidth(_ title: String, font: NSFont? = nil, button: Bool) -> CGFloat {
        let font = font ?? NSFont.systemFont(ofSize: 16, weight: .semibold)
        let width = (title as NSString).size(withAttributes: [.font: font]).width.rounded(.up)
        return width + (button ? 28 + 6 : 0)
    }

    // Softer bottom corners on the short closed strip, matching the camera notch's curve.
    static func surfaceRadius(height: CGFloat) -> CGFloat { min(28, height * 0.45) }
    static func shoulder(height: CGFloat) -> CGFloat { min(shoulder, height * 0.19) }

    static func capsuleGap(barHeight: CGFloat) -> CGFloat {
        guard barHeight.isFinite else { return 0 }
        return min(capsuleMargin, max(0, ((barHeight - 20) / 2).rounded(.down)))
    }

    static func capsuleRadius(height: CGFloat) -> CGFloat { min(max(0, height) / 2, 28) }

    static func capsuleBody(in rect: CGRect, gap: CGFloat) -> CGRect {
        let width = max(0, rect.width), height = max(0, rect.height)
        let side = min(shoulder(height: height), width / 2)
        let inset = min(max(0, gap), height / 2)
        return CGRect(x: rect.minX + side, y: rect.minY + inset,
                      width: width - side * 2, height: height - inset * 2)
    }

    static func capsulePath(in rect: CGRect, gap: CGFloat) -> CGPath {
        let body = capsuleBody(in: rect, gap: gap)
        let corner = min(capsuleRadius(height: body.height), body.width / 2)
        let handle = corner * (1 - 0.55228475)
        let (left, right, top, bottom) = (body.minX, body.maxX, body.minY, body.maxY)
        let path = CGMutablePath()
        path.move(to: CGPoint(x: left + corner, y: top))
        path.addLine(to: CGPoint(x: right - corner, y: top))
        path.addCurve(to: CGPoint(x: right, y: top + corner), control1: CGPoint(x: right - handle, y: top),
                      control2: CGPoint(x: right, y: top + handle))
        path.addLine(to: CGPoint(x: right, y: bottom - corner))
        path.addCurve(to: CGPoint(x: right - corner, y: bottom), control1: CGPoint(x: right, y: bottom - handle),
                      control2: CGPoint(x: right - handle, y: bottom))
        path.addLine(to: CGPoint(x: left + corner, y: bottom))
        path.addCurve(to: CGPoint(x: left, y: bottom - corner), control1: CGPoint(x: left + handle, y: bottom),
                      control2: CGPoint(x: left, y: bottom - handle))
        path.addLine(to: CGPoint(x: left, y: top + corner))
        path.addCurve(to: CGPoint(x: left + corner, y: top), control1: CGPoint(x: left, y: top + handle),
                      control2: CGPoint(x: left + handle, y: top))
        path.closeSubpath()
        return path
    }
}

struct NotchGeometry: Equatable {
    let screen: CGRect
    let cameraWidth: CGFloat
    let cameraHeight: CGFloat
    let isNotched: Bool
    let menuBarHeight: CGFloat
    let floatingGap: CGFloat?
    var compactSideRoom: CGFloat?

    init(screen: CGRect, safeAreaTop: CGFloat, cameraWidth: CGFloat, menuBarHeight: CGFloat = 24,
         compactSideRoom: CGFloat? = nil, barEdge: CGFloat = 0) {
        self.screen = screen
        let barHeight = menuBarHeight.isFinite ? min(64, max(16, menuBarHeight)) : 24
        let hasNotch = safeAreaTop.isFinite && safeAreaTop > 0 && cameraWidth.isFinite && cameraWidth > 0
        isNotched = hasNotch
        floatingGap = hasNotch ? nil : NotchLayout.capsuleGap(barHeight: barHeight)
        let edge = barEdge.isFinite ? min(max(0, barEdge), 1) : 0
        let stripHeight = max(barHeight - edge, (floatingGap ?? 0) * 2 + 12)
        let profileHeight = stripHeight - (floatingGap ?? 0) * 2 + NotchLayout.capsuleMargin * 2
        let width = min(hasNotch ? cameraWidth : 180 * profileHeight / 32, screen.width * 0.7)
        let height = hasNotch ? min(max(0, max(safeAreaTop, barHeight)), 64) : stripHeight
        self.cameraWidth = width
        cameraHeight = height
        self.menuBarHeight = max(height, barHeight)
        self.compactSideRoom = compactSideRoom
    }

    var floats: Bool { floatingGap != nil }
    var safeContentTop: CGFloat { floats ? NotchLayout.bottomInset : cameraHeight + 10 }

    func headerCameraGap(titleWidth: CGFloat) -> CGFloat {
        isNotched && contentWidth >= cameraWidth + 200
            && titleWidth <= (contentWidth - cameraWidth) / 2 ? cameraWidth : 0
    }

    func headerTopInset(cameraGap: CGFloat) -> CGFloat {
        if floats { return NotchLayout.bottomInset - (NotchLayout.headerHeight - 28) / 2 }
        return !isNotched || cameraGap > 0 ? 0 : safeContentTop
    }

    func headerRowHeight(cameraGap: CGFloat) -> CGFloat {
        cameraGap > 0 ? max(cameraHeight, NotchLayout.headerHeight) : NotchLayout.headerHeight
    }

    var stripHeight: CGFloat { cameraHeight }
    // Usage must remain visible even when menu-bar clearance cannot be measured.
    var restingWingWidth: CGFloat { 52 }
    var collapsed: CGSize {
        CGSize(width: min(screen.width - 24, cameraWidth + restingWingWidth * 2), height: stripHeight)
    }
    var expandedWidth: CGFloat { min(max(480, cameraWidth + 36), screen.width - 24) }
    var contentWidth: CGFloat { max(0, expandedWidth - NotchLayout.horizontalInset * 2) }

    func expandedSize(titleWidth: CGFloat) -> CGSize {
        let gap = headerCameraGap(titleWidth: titleWidth)
        let topInset = headerTopInset(cameraGap: gap)
        let rowHeight = headerRowHeight(cameraGap: gap)
        let height = topInset + rowHeight + NotchLayout.spacing + NotchLayout.compactContentHeight + NotchLayout.bottomInset
        return CGSize(width: expandedWidth, height: min(height, screen.height - 48))
    }

    func frame(for size: CGSize) -> CGRect {
        CGRect(x: screen.midX - size.width / 2, y: screen.maxY - size.height,
               width: size.width, height: size.height)
    }

    static func current() -> NotchGeometry? {
        let screens = NSScreen.screens
        guard !screens.isEmpty else { return nil }
        let main = NSScreen.main.flatMap { target in screens.firstIndex { $0 === target } } ?? 0
        let notched = screens.map { screen in
            guard let left = screen.auxiliaryTopLeftArea, let right = screen.auxiliaryTopRightArea else { return false }
            return screen.safeAreaInsets.top > 0 && right.minX > left.maxX
        }
        let builtInNotch = screens.indices.first { CGDisplayIsBuiltin(screens[$0].notchDisplayID) != 0 && notched[$0] }
        let index = builtInNotch ?? notched.firstIndex(of: true) ?? main
        let screen = screens[index]
        let cameraWidth: CGFloat
        if let left = screen.auxiliaryTopLeftArea, let right = screen.auxiliaryTopRightArea {
            cameraWidth = max(0, right.minX - left.maxX)
        } else {
            cameraWidth = 0
        }
        let measuredBarHeight = screen.frame.maxY - screen.visibleFrame.maxY
        let barHeight = (16...64).contains(measuredBarHeight) ? measuredBarHeight : NSStatusBar.system.thickness
        return NotchGeometry(screen: screen.frame, safeAreaTop: screen.safeAreaInsets.top,
                             cameraWidth: cameraWidth, menuBarHeight: barHeight,
                             barEdge: 1 / max(1, screen.backingScaleFactor))
    }
}

extension NSScreen {
    var notchDisplayID: CGDirectDisplayID {
        (deviceDescription[NSDeviceDescriptionKey("NSScreenNumber")] as? NSNumber)?.uint32Value ?? 0
    }
}

enum NotchMenuBarLayout {
    static func measuredSideRoom(screen: CGRect, cameraWidth: CGFloat, barHeight: CGFloat,
                                 menuItems: [CGRect], statusItems: [CGRect]) -> CGFloat? {
        let bar = CGRect(x: screen.minX, y: screen.maxY - barHeight, width: screen.width, height: barHeight)
        guard menuItems.contains(where: { $0.intersects(bar) }) else { return nil }
        return sideRoom(screen: screen, cameraWidth: cameraWidth, barHeight: barHeight,
                        occupied: menuItems + statusItems)
    }

    static func sideRoom(screen: CGRect, cameraWidth: CGFloat, barHeight: CGFloat,
                         occupied: [CGRect]) -> CGFloat? {
        let bar = CGRect(x: screen.minX, y: screen.maxY - barHeight, width: screen.width, height: barHeight)
        let camera = CGRect(x: screen.midX - cameraWidth / 2, y: bar.minY,
                            width: cameraWidth, height: barHeight)
        var left = screen.minX + 8
        var right = screen.maxX - 8
        for rect in occupied where rect.intersects(bar) {
            guard rect.minX.isFinite, rect.maxX.isFinite, rect.width > 0 else { return nil }
            if rect.intersects(camera) { return nil }
            if rect.maxX <= camera.minX { left = max(left, rect.maxX + 8) }
            if rect.minX >= camera.maxX { right = min(right, rect.minX - 8) }
        }
        return max(0, min(camera.minX - left, right - camera.maxX))
    }
}
