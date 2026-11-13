import AppKit
import PoupleCore
import SwiftUI
import UniformTypeIdentifiers

/// The files being added to the next message of a bubble. Each is uploaded to the core as soon as it is dropped, so
/// sending the message only has to name them.
@MainActor
final class AttachmentDraft: ObservableObject {
    struct Item: Identifiable {
        let ref: AttachmentRef
        /// A small preview of an image, so it shows at once.
        let thumbnail: NSImage?
        var id: String { ref.id }
    }

    @Published private(set) var items: [Item] = []
    @Published private(set) var uploading = false
    @Published private(set) var error: String?

    var ids: [String] { items.map(\.ref.id) }

    /// Reads the files the user dropped and uploads them. Stops at the first one that cannot be added and says why.
    func add(_ urls: [URL], upload: @escaping (String, Data) async throws -> AttachmentRef) {
        guard !urls.isEmpty else { return }
        error = nil
        uploading = true
        Task {
            defer { uploading = false }
            for url in urls {
                do {
                    try await addOne(url, upload: upload)
                } catch {
                    self.error = error.localizedDescription
                    return
                }
            }
        }
    }

    private func addOne(_ url: URL, upload: (String, Data) async throws -> AttachmentRef) async throws {
        let name = url.lastPathComponent
        let size = Int64((try? url.resourceValues(forKeys: [.fileSizeKey]).fileSize) ?? 0)
        if let problem = AttachmentRules.problem(name: name, size: size, alreadyAdded: items.count) {
            throw DraftError(Self.describe(problem, name: name))
        }
        // Read off the main actor: a file may be several megabytes.
        let data = try await Task.detached(priority: .userInitiated) { try Data(contentsOf: url) }.value
        let ref = try await upload(name, data)
        items.append(Item(ref: ref, thumbnail: ref.isImage ? NSImage(data: data) : nil))
    }

    func remove(_ id: String) {
        items.removeAll { $0.id == id }
    }

    /// Forgets what was added, once the message that carried it is sent.
    func clear() {
        items = []
        error = nil
    }

    private static func describe(_ problem: AttachmentProblem, name: String) -> String {
        switch problem {
        case .tooMany: return L10n.text(.attachmentTooMany, AttachmentRules.maxFiles)
        case .tooLarge: return L10n.text(.attachmentTooLarge, name)
        case .empty: return L10n.text(.attachmentEmpty, name)
        case .unsupported: return L10n.text(.attachmentUnsupported, name)
        }
    }

    private struct DraftError: LocalizedError {
        let errorDescription: String?
        init(_ message: String) { errorDescription = message }
    }
}

extension AttachmentDraft {
    /// The files of a drop, as URLs: what the Finder gives for a file dragged out of a window or off the desktop.
    nonisolated static func fileURLs(of providers: [NSItemProvider], then handle: @escaping @MainActor @Sendable ([URL]) -> Void) {
        let group = DispatchGroup()
        let lock = NSLock()
        var urls: [URL] = []
        for provider in providers where provider.hasItemConformingToTypeIdentifier(UTType.fileURL.identifier) {
            group.enter()
            _ = provider.loadObject(ofClass: URL.self) { url, _ in
                if let url { lock.lock(); urls.append(url); lock.unlock() }
                group.leave()
            }
        }
        group.notify(queue: .main) {
            let found = urls
            MainActor.assumeIsolated { handle(found) }
        }
    }
}

extension View {
    /// Takes the files dropped on the view, only when `enabled`. Where it is off, no native drop target is registered
    /// at all, so a development render (`--render`), which cannot draw one, shows the view as it is.
    @ViewBuilder func takingFiles(_ enabled: Bool, isTargeted: Binding<Bool>, perform: @escaping ([NSItemProvider]) -> Void) -> some View {
        if enabled {
            onDrop(of: [.fileURL], isTargeted: isTargeted) { providers in
                perform(providers)
                return true
            }
        } else {
            self
        }
    }
}
