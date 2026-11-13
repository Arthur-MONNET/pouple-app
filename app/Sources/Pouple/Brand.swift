import AppKit

/// The mascot SVGs bundled in `Contents/Resources/Mascots`. Missing in a development run: callers then show text only.
enum Mascot {
    /// The same drawing everywhere, with its navy outline.
    static func image(_ name: String) -> NSImage? {
        let key = name as NSString
        if let cached = cache.object(forKey: key) { return cached }
        guard let folder = Bundle.main.resourceURL?.appendingPathComponent("Mascots"),
              let image = NSImage(contentsOf: folder.appendingPathComponent("mascot-\(name).svg")) else { return nil }
        cache.setObject(image, forKey: key)
        return image
    }

    /// Views ask for the same few drawings on every redraw: decode each SVG once.
    private static let cache = NSCache<NSString, NSImage>()
}
