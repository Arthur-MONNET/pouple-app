import Foundation

public struct UpdateChecker {
    public enum Result: Equatable {
        case upToDate
        case available(version: String, downloadURL: URL)
    }

    public enum Failure: Error, Equatable {
        case invalidInstalledVersion
        case releaseUnavailable
        case archiveUnavailable
        case unexpectedResponse
    }

    private static let repository = "Arthur-MONNET/pouple-app"
    private static let archiveName = "Pouple.zip"
    private static let releaseEndpoint = URL(string: "https://api.github.com/repos/\(repository)/releases/latest")!

    private let load: (URLRequest) async throws -> (Data, URLResponse)

    public init() {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.timeoutIntervalForResource = 15
        let session = URLSession(configuration: configuration)
        self.init { request in try await session.data(for: request) }
    }

    init(load: @escaping (URLRequest) async throws -> (Data, URLResponse)) {
        self.load = load
    }

    public func check(installedVersion: String) async throws -> Result {
        guard let installed = ReleaseVersion(installedVersion) else { throw Failure.invalidInstalledVersion }

        var request = URLRequest(url: Self.releaseEndpoint, cachePolicy: .reloadIgnoringLocalCacheData)
        request.setValue("application/vnd.github+json", forHTTPHeaderField: "Accept")
        let (data, response) = try await load(request)
        guard let http = response as? HTTPURLResponse else { throw Failure.unexpectedResponse }
        if http.statusCode == 404 { throw Failure.releaseUnavailable }
        guard http.statusCode == 200,
              let release = try? JSONDecoder().decode(Release.self, from: data) else {
            throw Failure.unexpectedResponse
        }
        guard !release.draft, !release.prerelease else { throw Failure.releaseUnavailable }
        guard release.tagName.hasPrefix("v"),
              let published = ReleaseVersion(String(release.tagName.dropFirst())) else {
            throw Failure.unexpectedResponse
        }
        guard installed.components.lexicographicallyPrecedes(published.components) else { return .upToDate }
        guard release.assets.contains(where: { $0.name == Self.archiveName && $0.state == "uploaded" }) else {
            throw Failure.archiveUnavailable
        }

        // Build the link from the validated numeric tag rather than trusting a URL supplied in the response.
        let downloadURL = URL(string: "https://github.com/\(Self.repository)/releases/download/\(release.tagName)/\(Self.archiveName)")!
        return .available(version: String(release.tagName.dropFirst()), downloadURL: downloadURL)
    }

    private struct Release: Decodable {
        struct Asset: Decodable {
            let name: String
            let state: String
        }

        let tagName: String
        let draft: Bool
        let prerelease: Bool
        let assets: [Asset]

        enum CodingKeys: String, CodingKey {
            case tagName = "tag_name"
            case draft, prerelease, assets
        }
    }

    private struct ReleaseVersion {
        let components: [UInt]

        init?(_ version: String) {
            let parts = version.split(separator: ".", omittingEmptySubsequences: false)
            guard parts.count == 3 else { return nil }
            let numbers = parts.compactMap { part -> UInt? in
                guard !part.isEmpty, part == "0" || part.first != "0",
                      part.utf8.allSatisfy({ (48 ... 57).contains($0) }) else { return nil }
                return UInt(part)
            }
            guard numbers.count == parts.count else { return nil }
            components = numbers
        }
    }
}
