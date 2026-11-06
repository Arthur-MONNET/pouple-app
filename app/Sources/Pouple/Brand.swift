import AppKit
import SwiftUI

/// The mascot SVGs bundled in `Contents/Resources/Mascots`. Missing in a development run: callers then show text only.
enum Mascot {
    static func image(_ name: String) -> NSImage? {
        guard let folder = Bundle.main.resourceURL?.appendingPathComponent("Mascots") else { return nil }
        return NSImage(contentsOf: folder.appendingPathComponent("mascot-\(name).svg"))
    }
}

/// The octopus head from the app icon, small enough for a bubble header: coral dome, navy eyes.
struct OctopusHead: View {
    var size: CGFloat = 16

    var body: some View {
        Canvas { context, canvas in
            let unit = canvas.width / 20
            var head = Path()
            head.move(to: CGPoint(x: 1.5 * unit, y: 17 * unit))
            head.addCurve(to: CGPoint(x: 10 * unit, y: 1.5 * unit), control1: CGPoint(x: 1.5 * unit, y: 7 * unit), control2: CGPoint(x: 5 * unit, y: 1.5 * unit))
            head.addCurve(to: CGPoint(x: 18.5 * unit, y: 17 * unit), control1: CGPoint(x: 15 * unit, y: 1.5 * unit), control2: CGPoint(x: 18.5 * unit, y: 7 * unit))
            head.closeSubpath()
            context.fill(head, with: .color(Color(hex: 0xF2668B)))
            context.stroke(head, with: .color(Color(hex: 0x231F52)), lineWidth: 1.2 * unit)
            for x in [6.6, 13.4] as [CGFloat] {
                context.fill(Path(ellipseIn: CGRect(x: (x - 1.7) * unit, y: 9 * unit, width: 3.4 * unit, height: 3.4 * unit)), with: .color(Color(hex: 0x231F52)))
                context.fill(Path(ellipseIn: CGRect(x: (x - 0.2) * unit, y: 9.4 * unit, width: 1.1 * unit, height: 1.1 * unit)), with: .color(.white))
            }
        }
        .frame(width: size, height: size)
        .accessibilityHidden(true)
    }
}
