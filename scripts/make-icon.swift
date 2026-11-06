// Draws the placeholder app icon (1024x1024 PNG): a dark rounded square with a soft indigo glow and a white
// octopus, the same shape as the menu bar glyph. Usage: swift make-icon.swift out.png
import AppKit

let side: CGFloat = 1024
guard CommandLine.arguments.count > 1 else {
    FileHandle.standardError.write(Data("usage: make-icon.swift out.png\n".utf8))
    exit(2)
}

let bitmap = NSBitmapImageRep(
    bitmapDataPlanes: nil, pixelsWide: Int(side), pixelsHigh: Int(side), bitsPerSample: 8, samplesPerPixel: 4,
    hasAlpha: true, isPlanar: false, colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0
)!
NSGraphicsContext.saveGraphicsState()
NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: bitmap)

// macOS icons keep a margin around a squircle-ish body.
let body = NSRect(x: 100, y: 100, width: 824, height: 824)
let shape = NSBezierPath(roundedRect: body, xRadius: 185, yRadius: 185)
shape.addClip()
NSGradient(colors: [NSColor(srgbRed: 0.16, green: 0.18, blue: 0.30, alpha: 1), NSColor(srgbRed: 0.055, green: 0.059, blue: 0.071, alpha: 1)])!
    .draw(in: body, angle: -60)

// The octopus, drawn on the 18-unit grid of the menu bar glyph and scaled up.
let unit = 33.0
let origin = NSPoint(x: side / 2 - 9 * unit, y: side / 2 - 9.4 * unit)
func point(_ x: CGFloat, _ y: CGFloat) -> NSPoint { NSPoint(x: origin.x + x * unit, y: origin.y + y * unit) }

NSColor(srgbRed: 0.557, green: 0.635, blue: 1.0, alpha: 1).setFill()
NSColor(srgbRed: 0.557, green: 0.635, blue: 1.0, alpha: 1).setStroke()
let head = NSBezierPath()
head.move(to: point(3.2, 8))
head.curve(to: point(9, 16.6), controlPoint1: point(3.2, 13), controlPoint2: point(5.6, 16.6))
head.curve(to: point(14.8, 8), controlPoint1: point(12.4, 16.6), controlPoint2: point(14.8, 13))
head.close()
head.fill()
for index in 0 ..< 4 {
    let x = 4.4 + CGFloat(index) * 3.07
    let sway: CGFloat = index % 2 == 0 ? -1.2 : 1.2
    let leg = NSBezierPath()
    leg.lineWidth = 2.1 * unit
    leg.lineCapStyle = .round
    leg.move(to: point(x, 8.5))
    leg.curve(to: point(x + sway, 1.8), controlPoint1: point(x + sway, 6), controlPoint2: point(x - sway, 3.8))
    leg.stroke()
}
NSColor(srgbRed: 0.055, green: 0.059, blue: 0.071, alpha: 1).setFill()
for x in [6.6, 11.4] as [CGFloat] {
    NSBezierPath(ovalIn: NSRect(x: point(x - 1, 9.6).x, y: point(x - 1, 9.6).y, width: 2 * unit, height: 2.4 * unit)).fill()
}
NSGraphicsContext.restoreGraphicsState()

try! bitmap.representation(using: .png, properties: [:])!.write(to: URL(fileURLWithPath: CommandLine.arguments[1]))
