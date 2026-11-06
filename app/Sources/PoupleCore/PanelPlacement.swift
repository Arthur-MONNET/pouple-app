import CoreGraphics

public enum Corner: String, Codable, CaseIterable, Sendable {
    case topLeft, topRight, bottomLeft, bottomRight

    public var isTop: Bool { self == .topLeft || self == .topRight }
    public var isLeft: Bool { self == .topLeft || self == .bottomLeft }
}

/// Where the bubble panel sits: always against one corner of a screen. Coordinates are AppKit's (origin bottom-left).
public enum PanelPlacement {
    /// The corner of `area` closest to `point`: the quadrant the point is in.
    public static func nearestCorner(to point: CGPoint, in area: CGRect) -> Corner {
        let left = point.x < area.midX
        let top = point.y >= area.midY
        switch (top, left) {
        case (true, true): return .topLeft
        case (true, false): return .topRight
        case (false, true): return .bottomLeft
        case (false, false): return .bottomRight
        }
    }

    /// The frame of a panel of `size` against `corner` of `area`, `margin` away from both edges.
    /// A panel taller or wider than the area allows is reduced so it never leaves the screen.
    public static func frame(corner: Corner, size: CGSize, in area: CGRect, margin: CGFloat) -> CGRect {
        let fitted = CGSize(
            width: min(size.width, max(area.width - 2 * margin, 1)),
            height: min(size.height, max(area.height - 2 * margin, 1))
        )
        let x = corner.isLeft ? area.minX + margin : area.maxX - margin - fitted.width
        let y = corner.isTop ? area.maxY - margin - fitted.height : area.minY + margin
        return CGRect(origin: CGPoint(x: x, y: y), size: fitted)
    }

    /// The screen a panel belongs to: the one it overlaps most, or the closest one when it overlaps none.
    public static func screenIndex(for frame: CGRect, in areas: [CGRect]) -> Int? {
        guard !areas.isEmpty else { return nil }
        let overlaps = areas.map { area -> CGFloat in
            let shared = area.intersection(frame)
            return shared.isNull ? 0 : shared.width * shared.height
        }
        if let best = overlaps.max(), best > 0 { return overlaps.firstIndex(of: best) }
        let center = CGPoint(x: frame.midX, y: frame.midY)
        let distances = areas.map { area -> CGFloat in
            let dx = max(area.minX - center.x, 0, center.x - area.maxX)
            let dy = max(area.minY - center.y, 0, center.y - area.maxY)
            return dx * dx + dy * dy
        }
        return distances.indices.min { distances[$0] < distances[$1] }
    }
}
