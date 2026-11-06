// Renders the brand SVG into an .iconset folder (every size macOS wants, 16 to 1024 px) for `iconutil`.
// Usage: swift make-icon.swift brand/app-icon.svg out.iconset
// NSImage draws SVG natively on macOS 13+, so no third-party tool is needed. Each size is rendered from the
// vector, not scaled down from a bitmap, so small sizes stay crisp.
import AppKit

guard CommandLine.arguments.count > 2, let svg = NSImage(contentsOf: URL(fileURLWithPath: CommandLine.arguments[1])) else {
    FileHandle.standardError.write(Data("usage: make-icon.swift app-icon.svg out.iconset\n".utf8))
    exit(2)
}
let folder = URL(fileURLWithPath: CommandLine.arguments[2])
try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)

func png(pixels: Int) -> Data {
    let rep = NSBitmapImageRep(
        bitmapDataPlanes: nil, pixelsWide: pixels, pixelsHigh: pixels, bitsPerSample: 8, samplesPerPixel: 4,
        hasAlpha: true, isPlanar: false, colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0
    )!
    NSGraphicsContext.saveGraphicsState()
    NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: rep)
    NSGraphicsContext.current?.imageInterpolation = .high
    svg.draw(in: NSRect(x: 0, y: 0, width: pixels, height: pixels))
    NSGraphicsContext.restoreGraphicsState()
    return rep.representation(using: .png, properties: [:])!
}

for size in [16, 32, 128, 256, 512] {
    try png(pixels: size).write(to: folder.appendingPathComponent("icon_\(size)x\(size).png"))
    try png(pixels: size * 2).write(to: folder.appendingPathComponent("icon_\(size)x\(size)@2x.png"))
}
