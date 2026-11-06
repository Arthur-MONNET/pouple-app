import SwiftUI

/// What the window shows instead of the web page while the core is starting, failed, or the page is loading.
@MainActor
final class PlaceholderModel: ObservableObject {
    enum Mode: Equatable {
        case starting
        case loading
        case failed(String)
    }

    @Published var mode: Mode = .starting
    var retry: () -> Void = {}
}

struct PlaceholderView: View {
    @ObservedObject var model: PlaceholderModel

    var body: some View {
        VStack(spacing: 14) {
            switch model.mode {
            case .starting:
                mascot("sleep")
                ProgressView().controlSize(.large)
                Text("Starting Pouple…").font(.system(size: 15, weight: .medium))
            case .loading:
                mascot("sleep")
                ProgressView().controlSize(.large)
                Text("Loading…").font(.system(size: 15, weight: .medium))
            case let .failed(message):
                if let image = Mascot.image("oops") {
                    MascotStage(image: image)
                } else {
                    Image(systemName: "exclamationmark.triangle").font(.system(size: 28)).foregroundStyle(Theme.warn)
                }
                Text("Pouple could not start").font(.system(size: 15, weight: .semibold))
                Text(message)
                    .font(.system(size: 12))
                    .foregroundStyle(Theme.muted)
                    .multilineTextAlignment(.center)
                    .frame(maxWidth: 420)
                    .textSelection(.enabled)
                Button("Retry") { model.retry() }.buttonStyle(PillButtonStyle(role: .primary))
            }
        }
        .foregroundStyle(.white.opacity(0.9))
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .background(WindowBackground.color)
        .environment(\.colorScheme, .dark)
    }
}

private extension PlaceholderView {
    @ViewBuilder func mascot(_ name: String) -> some View {
        if let image = Mascot.image(name) { MascotStage(image: image) }
    }
}

/// The mascot on its dark stage, with a hairline.
private struct MascotStage: View {
    let image: NSImage

    var body: some View {
        Image(nsImage: image)
            .resizable()
            .scaledToFit()
            .frame(width: 140, height: 140)
            .padding(20)
            .background(RoundedRectangle(cornerRadius: 28, style: .continuous).fill(Theme.stage))
            .overlay(RoundedRectangle(cornerRadius: 28, style: .continuous).strokeBorder(Color(hex: 0x2E323A)))
    }
}

enum WindowBackground {
    /// The web UI's background, so there is never a white flash.
    static let nsColor = NSColor(srgbRed: 0x0e / 255, green: 0x0f / 255, blue: 0x12 / 255, alpha: 1)
    static let color = Color(nsColor: nsColor)
}
