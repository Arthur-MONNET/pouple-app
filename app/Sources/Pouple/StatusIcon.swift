import AppKit

/// The menu bar glyph, drawn in code: a small octopus. It is a template image, so the system tints it.
enum StatusIcon {
    static func image(size: CGFloat = 18) -> NSImage {
        let image = NSImage(size: NSSize(width: size, height: size), flipped: false) { rect in
            let unit = rect.width / 18
            NSColor.black.setFill()
            NSColor.black.setStroke()

            // Head and mantle.
            let head = NSBezierPath()
            head.move(to: NSPoint(x: 3.2 * unit, y: 8 * unit))
            head.curve(to: NSPoint(x: 9 * unit, y: 16.6 * unit), controlPoint1: NSPoint(x: 3.2 * unit, y: 13 * unit), controlPoint2: NSPoint(x: 5.6 * unit, y: 16.6 * unit))
            head.curve(to: NSPoint(x: 14.8 * unit, y: 8 * unit), controlPoint1: NSPoint(x: 12.4 * unit, y: 16.6 * unit), controlPoint2: NSPoint(x: 14.8 * unit, y: 13 * unit))
            head.close()
            head.fill()

            // Tentacles.
            for index in 0 ..< 4 {
                let x = (4.4 + CGFloat(index) * 3.07) * unit
                let sway: CGFloat = index % 2 == 0 ? -1.2 : 1.2
                let leg = NSBezierPath()
                leg.lineWidth = 2.1 * unit
                leg.lineCapStyle = .round
                leg.move(to: NSPoint(x: x, y: 8.5 * unit))
                leg.curve(
                    to: NSPoint(x: x + sway * unit, y: 1.8 * unit),
                    controlPoint1: NSPoint(x: x + sway * unit, y: 6 * unit),
                    controlPoint2: NSPoint(x: x - sway * unit, y: 3.8 * unit)
                )
                leg.stroke()
            }

            // Eyes, cut out of the head so the menu bar shows through.
            NSGraphicsContext.current?.compositingOperation = .destinationOut
            for x in [6.6, 11.4] as [CGFloat] {
                NSBezierPath(ovalIn: NSRect(x: (x - 1) * unit, y: 9.6 * unit, width: 2 * unit, height: 2.4 * unit)).fill()
            }
            return true
        }
        image.isTemplate = true
        image.accessibilityDescription = "Pouple"
        return image
    }
}
