import PoupleCore
import AppKit

/// The menu bar glyph, drawn in code: a small octopus. It is a template image, so the system tints it.
enum StatusIcon {
    /// - Parameter badge: adds a dot at the top right, the sign that Pouple is working.
    static func image(size: CGFloat = 18, badge: Bool = false) -> NSImage {
        let image = NSImage(size: NSSize(width: size, height: size), flipped: false) { rect in
            let unit = rect.width / 18
            NSColor.black.setFill()
            NSColor.black.setStroke()

            // Dome head with a flat base, wide enough to read at 18 px.
            let head = NSBezierPath()
            head.move(to: NSPoint(x: 2.2 * unit, y: 7 * unit))
            head.curve(to: NSPoint(x: 9 * unit, y: 16.8 * unit), controlPoint1: NSPoint(x: 2.2 * unit, y: 12.6 * unit), controlPoint2: NSPoint(x: 5 * unit, y: 16.8 * unit))
            head.curve(to: NSPoint(x: 15.8 * unit, y: 7 * unit), controlPoint1: NSPoint(x: 13 * unit, y: 16.8 * unit), controlPoint2: NSPoint(x: 15.8 * unit, y: 12.6 * unit))
            head.close()
            head.fill()

            // Four short tentacles, curling outwards at the tips.
            for index in 0 ..< 4 {
                let x = (3.9 + CGFloat(index) * 3.4) * unit
                let curl: CGFloat = index < 2 ? -1 : 1
                let leg = NSBezierPath()
                leg.lineWidth = 2.5 * unit
                leg.lineCapStyle = .round
                leg.move(to: NSPoint(x: x, y: 7.5 * unit))
                leg.curve(
                    to: NSPoint(x: x + curl * 0.9 * unit, y: 2.2 * unit),
                    controlPoint1: NSPoint(x: x, y: 4.5 * unit),
                    controlPoint2: NSPoint(x: x + curl * 0.2 * unit, y: 2.6 * unit)
                )
                leg.stroke()
            }

            // Two round eyes, cut out of the head so the menu bar shows through.
            NSGraphicsContext.current?.compositingOperation = .destinationOut
            for x in [6.3, 11.7] as [CGFloat] {
                NSBezierPath(ovalIn: NSRect(x: (x - 1.35) * unit, y: 8.6 * unit, width: 2.7 * unit, height: 3.2 * unit)).fill()
            }

            if badge {
                // A ring is cut out of the head first, so the dot stays readable where it overlaps it.
                let center = NSPoint(x: 15.2 * unit, y: 15.2 * unit)
                NSBezierPath(ovalIn: NSRect(x: center.x - 3.4 * unit, y: center.y - 3.4 * unit, width: 6.8 * unit, height: 6.8 * unit)).fill()
                NSGraphicsContext.current?.compositingOperation = .sourceOver
                NSBezierPath(ovalIn: NSRect(x: center.x - 2.3 * unit, y: center.y - 2.3 * unit, width: 4.6 * unit, height: 4.6 * unit)).fill()
            }
            return true
        }
        image.isTemplate = true
        image.accessibilityDescription = L10n.text(.handleName)
        return image
    }
}
