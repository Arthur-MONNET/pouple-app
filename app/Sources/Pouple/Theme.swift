import SwiftUI

/// Colors and sizes shared by the views; the palette follows the Pouple web app.
enum Theme {
    static let cardWidth: CGFloat = 392
    /// Room around the cards so shadows are not clipped by the panel edge.
    static let panelPadding: CGFloat = 14

    static let card = Color(red: 0.055, green: 0.059, blue: 0.071)
    static let raised = Color(red: 0.106, green: 0.118, blue: 0.141)
    static let line = Color.white.opacity(0.09)
    static let accent = Color(red: 0.557, green: 0.635, blue: 1.0)
    static let warn = Color(red: 0.902, green: 0.706, blue: 0.353)
    static let ok = Color(red: 0.365, green: 0.788, blue: 0.561)
    static let fail = Color(red: 0.937, green: 0.42, blue: 0.42)
    static let muted = Color.white.opacity(0.62)
    static let dim = Color.white.opacity(0.42)
}

struct PillButtonStyle: ButtonStyle {
    enum Role { case primary, secondary, quiet }
    var role: Role = .secondary

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .font(.system(size: 12, weight: role == .primary ? .semibold : .regular))
            .foregroundStyle(role == .primary ? Color.black.opacity(0.85) : Color.white.opacity(role == .quiet ? 0.6 : 0.9))
            .padding(.horizontal, 11)
            .frame(height: 26)
            .background(
                Capsule().fill(role == .primary ? Theme.accent : (role == .quiet ? Color.clear : Color.white.opacity(0.1)))
            )
            .opacity(configuration.isPressed ? 0.7 : 1)
            .contentShape(Capsule())
    }
}
