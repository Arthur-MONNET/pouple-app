import Foundation

/// One reminder the user ticked, with the local moment they want for it ("2026-11-20T09:00", read on the Mac's clock).
public struct ReminderPick: Equatable, Sendable {
    public let id: String
    public let at: String

    public init(id: String, at: String) {
        self.id = id
        self.at = at
    }
}

/// What the user did so far on a suggestion of reminders: the options they ticked and the moment wanted for each.
/// A new suggestion starts over, with nothing ticked and each option at the moment proposed.
public struct ReminderDraft: Equatable, Sendable {
    public let offer: ReminderOffer
    public private(set) var ticked: Set<String> = []
    public private(set) var times: [String: Date]

    public init(offer: ReminderOffer) {
        self.offer = offer
        times = Dictionary(uniqueKeysWithValues: offer.items.map { ($0.id, $0.at) })
    }

    public func isTicked(_ id: String) -> Bool { ticked.contains(id) }

    public func time(of id: String) -> Date { times[id] ?? .distantPast }

    public mutating func toggle(_ id: String) {
        if ticked.contains(id) { ticked.remove(id) } else { ticked.insert(id) }
    }

    /// Changing the moment of an option is choosing it.
    public mutating func setTime(_ date: Date, of id: String) {
        times[id] = date
        ticked.insert(id)
    }

    /// The ticked options, in the order they are listed.
    public var chosen: [ReminderOffer.Item] { offer.items.filter { ticked.contains($0.id) } }

    /// Whether every ticked option is still in the future, which is what the core requires.
    public func allUpcoming(now: Date = Date()) -> Bool { chosen.allSatisfy { time(of: $0.id) > now } }

    public func canConfirm(now: Date = Date()) -> Bool { !chosen.isEmpty && allUpcoming(now: now) }

    /// What to send to the core; nil when there is nothing to confirm yet.
    public func picks(now: Date = Date()) -> [ReminderPick]? {
        guard canConfirm(now: now) else { return nil }
        return chosen.map { ReminderPick(id: $0.id, at: Self.localInput(time(of: $0.id))) }
    }

    /// A date and time without zone, to the minute: what the core reads on its own clock, which is this Mac's.
    public static func localInput(_ date: Date, calendar: Calendar = .current) -> String {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.calendar = calendar
        formatter.timeZone = calendar.timeZone
        formatter.dateFormat = "yyyy-MM-dd'T'HH:mm"
        return formatter.string(from: date)
    }
}
