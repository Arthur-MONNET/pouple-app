import SwiftUI

/// Colors and sizes shared by the views; the palette follows the Pouple web app.
enum Theme {
    static let cardWidth: CGFloat = 392
    /// Room around the cards so shadows are not clipped by the panel edge.
    static let panelPadding: CGFloat = 14

    /// Bubble surface, and the darker wells (quotes, fields) set into it.
    static let card = Color(hex: 0x1B1E24)
    static let raised = Color(hex: 0x15171B)
    static let line = Color(hex: 0x3A3F48)
    /// Coral: the accent, and the fill of primary buttons.
    static let accent = Color(hex: 0xF2668B)
    /// Text on a coral fill.
    static let onAccent = Color(hex: 0x0E0F12)
    static let warn = Color(hex: 0xFFC857)
    static let ok = Color(hex: 0x4CC38A)
    static let fail = Color(red: 0.937, green: 0.42, blue: 0.42)
    static let muted = Color(hex: 0xA4A9B3)
    static let dim = Color.white.opacity(0.42)
}

struct PillButtonStyle: ButtonStyle {
    enum Role { case primary, secondary, quiet }
    var role: Role = .secondary

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.system(size: 12, weight: role == .primary ? .semibold : .regular))
            .foregroundStyle(role == .primary ? Theme.onAccent : Color.white.opacity(role == .quiet ? 0.6 : 0.9))
            .padding(.horizontal, 11)
            .frame(height: 26)
            .background(
                Capsule().fill(role == .primary ? Theme.accent : (role == .quiet ? Color.clear : Color.white.opacity(0.1)))
            )
            .opacity(configuration.isPressed ? 0.7 : 1)
            .contentShape(Capsule())
    }
}

extension Color {
    init(hex: UInt32) {
        self.init(
            .sRGB,
            red: Double((hex >> 16) & 0xFF) / 255,
            green: Double((hex >> 8) & 0xFF) / 255,
            blue: Double(hex & 0xFF) / 255
        )
    }
}
