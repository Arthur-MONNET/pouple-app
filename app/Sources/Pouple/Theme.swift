import SwiftUI

/// Colors and sizes shared by the views; the palette follows the Pouple web app.
enum Theme {
    static let cardWidth: CGFloat = 392
    /// Room around the cards so shadows are not clipped by the panel edge.
    static let panelPadding: CGFloat = 14

    /// The mascot sits on the top edge of an expanded bubble: its size, and how far it rises above the card.
    static let mascotSize: CGFloat = 68
    static let mascotOverhang: CGFloat = 44

    /// Bubble surface: the same background as the app window.
    static let card = Color(hex: 0x0E0F12)
    /// Fills set into the card, from the quietest (quotes, code) to the one that holds controls (options, fields).
    static let raised = Color(hex: 0x15171B)
    static let tile = Color(hex: 0x1B1E24)
    static let line = Color(hex: 0x3A3F48)
    /// Coral: the accent, and the fill of primary buttons.
    static let accent = Color(hex: 0xF2668B)
    /// Text on a coral fill.
    static let onAccent = Color(hex: 0x0E0F12)
    static let warn = Color(hex: 0xFFC857)
    static let ok = Color(hex: 0x4CC38A)
    /// Errors only, always next to a word or an icon. Red-orange, far from the coral accent.
    static let fail = Color(hex: 0xFF5A36)
    static let muted = Color(hex: 0xA4A9B3)
    static let dim = Color(hex: 0x868B95)
    /// The dark stage the mascot sits on.
    static let stage = Color(hex: 0x1E1D3F)
}

struct PillButtonStyle: ButtonStyle {
    /// `destructive` (Stop, Delete) is outlined in `fail`, never filled; `primary` is the only filled button.
    enum Role { case primary, secondary, quiet, destructive }
    var role: Role = .secondary

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.system(size: 12, weight: role == .primary ? .semibold : .regular))
            .foregroundStyle(role == .primary ? Theme.onAccent : (role == .destructive ? Theme.fail : Color.white.opacity(role == .quiet ? 0.6 : 0.9)))
            .padding(.horizontal, 11)
            .frame(height: 26)
            .background(
                Capsule().fill(role == .primary ? Theme.accent : (role == .quiet || role == .destructive ? Color.clear : Color.white.opacity(0.1)))
            )
            .overlay(Capsule().strokeBorder(Theme.fail, lineWidth: role == .destructive ? 1 : 0))
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
