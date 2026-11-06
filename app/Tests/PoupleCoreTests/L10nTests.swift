import XCTest
@testable import PoupleCore

final class L10nTests: XCTestCase {
    func testFrenchAndEnglishHaveExactlyTheSameKeysAndNoEmptyValue() {
        let all = Set(L10n.Key.allCases)
        for language in Language.allCases {
            let table = L10n.table(language)
            XCTAssertEqual(Set(table.keys), all, "\(language) is missing or has extra keys")
            for (key, value) in table {
                XCTAssertFalse(value.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty, "\(language).\(key) is empty")
            }
        }
    }

    func testBothLanguagesTakeTheSamePlaceholders() {
        func placeholders(_ text: String) -> [String] {
            text.components(separatedBy: "%").dropFirst().map { String($0.prefix(1)) }.sorted()
        }
        for key in L10n.Key.allCases {
            XCTAssertEqual(placeholders(L10n.french[key] ?? ""), placeholders(L10n.english[key] ?? ""), "\(key)")
        }
    }

    func testFrenchPunctuationUsesNonBreakingSpaces() {
        for (key, value) in L10n.french {
            let typeset = French.typographic(value)
            for bad in [" :", " ;", " !", " ?", "« ", " »"] {
                XCTAssertFalse(typeset.contains(bad), "\(key) still has a breaking space before or inside \(bad)")
            }
        }
        XCTAssertEqual(L10n.text(.bubbleApplyIn, "vision", language: .fr), "Appliquer cette modification dans vision\u{202F}?")
        XCTAssertEqual(L10n.text(.failBinaryMissing, "/x", language: .fr), "Le moteur est introuvable\u{00A0}: /x")
        XCTAssertEqual(French.typographic("« oui »"), "«\u{00A0}oui\u{00A0}»")
        XCTAssertEqual(L10n.text(.bubbleApplyIn, "vision", language: .en), "Apply this change in vision?")
    }

    func testTheNamedTonesAreThere() {
        XCTAssertEqual(L10n.text(.menuOpen, language: .fr), "Ouvrir Pouple")
        XCTAssertEqual(L10n.text(.menuShowBubbles, language: .fr), "Afficher les bulles")
        XCTAssertEqual(L10n.text(.menuStartAtLogin, language: .fr), "Lancer à l'ouverture de session")
        XCTAssertEqual(L10n.text(.menuQuit, language: .fr), "Quitter Pouple")
        XCTAssertEqual(L10n.text(.badgeNeedsAnswer, language: .fr), "À toi de décider")
        XCTAssertEqual(L10n.text(.placeholderStarting, language: .fr), "Pouple fait la sieste…")
    }

    func testPluralsFollowEachLanguagesRule() {
        func steps(_ count: Int, _ language: Language) -> String {
            L10n.plural(one: .activityOne, other: .activityOther, count: count, language: language)
        }
        XCTAssertEqual(steps(0, .fr), "Activité · 0 étape")
        XCTAssertEqual(steps(1, .fr), "Activité · 1 étape")
        XCTAssertEqual(steps(2, .fr), "Activité · 2 étapes")
        XCTAssertEqual(steps(0, .en), "Activity · 0 steps")
        XCTAssertEqual(steps(1, .en), "Activity · 1 step")
        XCTAssertEqual(steps(5, .en), "Activity · 5 steps")
    }

    func testRelativeTimeInBothLanguages() {
        let now = Date(timeIntervalSince1970: 1_800_000_000)
        func ago(_ seconds: TimeInterval, _ language: Language) -> String {
            L10n.relativeTime(from: now.addingTimeInterval(-seconds), to: now, language: language)
        }
        XCTAssertEqual(ago(10, .fr), "à l'instant")
        XCTAssertEqual(ago(10, .en), "just now")
        XCTAssertEqual(ago(5 * 60, .fr), "il y a 5 min")
        XCTAssertEqual(ago(5 * 60, .en), "5 min ago")
        XCTAssertEqual(ago(3 * 3600, .fr), "il y a 3 h")
        XCTAssertEqual(ago(2 * 86400, .en), "2 d ago")
        // A clock that is slightly behind never gives a negative time.
        XCTAssertEqual(L10n.relativeTime(from: now.addingTimeInterval(30), to: now, language: .fr), "à l'instant")
    }

    func testDatesAreFormattedWithTheLanguagesLocale() {
        let date = Date(timeIntervalSince1970: 1_793_961_720) // 2026-11-06 10:42 UTC
        let utc = TimeZone(identifier: "UTC")!
        let fr = L10n.dateTime(date, language: .fr, timeZone: utc)
        let en = L10n.dateTime(date, language: .en, timeZone: utc)
        XCTAssertTrue(fr.contains("nov"), fr)
        XCTAssertTrue(fr.contains("10:42"), fr)
        XCTAssertTrue(en.contains("Nov"), en)
        XCTAssertTrue(en.contains("10:42"), en)
    }

    func testCoreFailuresAreWordedInTheRequestedLanguage() {
        XCTAssertEqual(CoreFailure.timeout(seconds: 1).message(language: .fr), "Le moteur n'a pas répondu en 1 seconde")
        XCTAssertEqual(CoreFailure.timeout(seconds: 15).message(language: .fr), "Le moteur n'a pas répondu en 15 secondes")
        XCTAssertEqual(CoreFailure.timeout(seconds: 15).message(language: .en), "The core did not become ready within 15 seconds")
        XCTAssertEqual(CoreFailure.stopped(status: nil).message(language: .en), "The core stopped unexpectedly (exit status unknown)")
        XCTAssertEqual(CoreFailure.readyLine(.invalidPort(0)).message(language: .en), "The core announced an unusable port (0)")
        XCTAssertEqual(CoreFailure.other("boom").message(language: .fr), "boom")
    }

    func testLanguageCodesAreParsedLeniently() {
        XCTAssertEqual(Language(code: "fr"), .fr)
        XCTAssertEqual(Language(code: "en-US"), .en)
        XCTAssertEqual(Language(code: "FR_ca"), .fr)
        XCTAssertNil(Language(code: "de"))
        XCTAssertNil(Language(code: ""))
    }

    func testTheSetupStateCarriesTheLanguageTheCoreReports() throws {
        func decode(_ json: String) throws -> SetupState { try JSONDecoder().decode(SetupState.self, from: Data(json.utf8)) }
        XCTAssertEqual(try decode(#"{"setupComplete":true,"steps":[],"language":"en"}"#).language, .en)
        XCTAssertEqual(try decode(#"{"setupComplete":true,"steps":[]}"#).language, nil)
        XCTAssertEqual(try decode(#"{"setupComplete":false,"language":"de"}"#).language, nil)
    }
}

@MainActor
final class LanguageModelTests: XCTestCase {
    override func tearDown() {
        L10n.current = .fr
        super.tearDown()
    }

    func testChangingTheLanguageUpdatesWhatViewsRead() {
        let model = LanguageModel()
        XCTAssertEqual(model.language, .fr)
        XCTAssertEqual(L10n.text(.menuQuit), "Quitter Pouple")
        model.set(.en)
        XCTAssertEqual(model.language, .en)
        XCTAssertEqual(L10n.text(.menuQuit), "Quit Pouple")
    }

    func testTheLastLanguageIsRememberedForTheNextLaunch() {
        let defaults = UserDefaults(suiteName: "pouple-tests-\(UUID().uuidString)")!
        LanguageModel(defaults: defaults).set(.en)
        XCTAssertEqual(LanguageModel(defaults: defaults).language, .en)
    }

    func testTheMonitorReportsTheCoresLanguageAndForgetsItWhenDetached() async throws {
        let monitor = SetupMonitor(interval: .milliseconds(10))
        monitor.attach { SetupState(setupComplete: true, language: .en) }
        try await waitUntil { monitor.language == .en }
        monitor.attach(fetch: nil)
        XCTAssertNil(monitor.language)
    }
}
