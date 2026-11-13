import Foundation

/// Everything the Swift app writes itself, in French and English. What the core sends (conversation entries,
/// summaries, notices) is already localized by the core and never goes through here.
///
/// Values may hold `%@` / `%d` placeholders. French values are written with plain spaces; `French.typographic`
/// turns the ones before `: ; ! ?` and inside « » into the right non-breaking spaces when they are read.
public enum L10n {
    public enum Key: String, CaseIterable, Sendable {
        // Menu bar item
        case menuOpen, menuHideBubbles, menuShowBubbles, menuResetPosition, menuStartAtLogin, menuQuit, menuLanguage
        case statusStarting, statusUnavailable, statusChecking, statusConnected, statusSetupNeeded
        case loginWaitingApproval, loginUnavailable, loginWithDetail, loginErrorTitle, loginErrorBody
        case languageChangeFailed
        // Application menus
        case appAbout, appHide, appHideOthers, appQuit
        case editMenu, undo, redo, cut, copy, paste, selectAll
        case viewMenu, reload, windowMenu, minimize, close
        // Updates
        case updateCheck, updateChecking, updateAvailable, updateOpenInstructions, updateInstallInstructions
        case updateUpToDate, updateInstalledVersion, updateUnavailable, updateInvalidInstalledVersion
        case updateReleaseUnavailable, updateFailed, updateTryAgain, updateBrowserFailed
        case updateNow, updateNotFromSource, updateCheckoutMissing, updateTerminalFailed
        case scriptQuitting, scriptFetching, scriptBuilding, scriptDone, scriptNotQuit, scriptBadOrigin, scriptPullFailed, scriptInstallFailed, scriptClose
        // Window, placeholder, dialogs
        case windowTitle, placeholderStarting, placeholderLoading, placeholderFailedTitle, retry
        case dialogOK, dialogCancel
        // Core and API errors
        case failStopped, failStatusUnknown, failClosedBeforeReady, failTimeoutOne, failTimeoutOther, failBinaryMissing
        case failReadyMalformed, failNotReady, failInvalidPort
        case apiAnswered, apiUnexpected
        // Bubbles
        case bubbleCollapse, bubbleClose, bubbleSomethingWrong, bubbleRetry, bubbleDone
        case bubbleApply, bubbleNotNow, bubbleOpen, bubbleCopy, bubbleSuggestedReply, bubbleApplyIn
        case badgeApprove, badgeNeedsAnswer, badgeFailed, badgeApplied, badgeAnswerReady, badgeWorking
        case bubbleIgnore, bubbleStop, bubbleWorking
        case activityOne, activityOther
        case handleName, handleWorking, handleOverflow, handleDrag, handleOffline
        case replyPlaceholder, replySend, wizardOther, wizardBack, wizardNext, wizardSend, wizardSeveral
        // Titles of requests
        case titleNoteToSelf, titleDirect, titleGroup, titleBroadcast, titleKeyword, titleThread, titleChannel, titleAction
        // Relative time
        case timeNow, timeMinutes, timeHours, timeDays
    }

    private static let currentLanguage = LockedBox(Language.default)

    /// The language `text(_:)` uses when none is given. Set by `LanguageModel`.
    public static var current: Language {
        get { currentLanguage.mutate { $0 } }
        set { currentLanguage.mutate { $0 = newValue } }
    }

    public static func text(_ key: Key, _ arguments: CVarArg..., language: Language? = nil) -> String {
        let language = language ?? current
        let raw = table(language)[key] ?? ""
        let value = language == .fr ? French.typographic(raw) : raw
        return arguments.isEmpty ? value : String(format: value, locale: language.locale, arguments: arguments)
    }

    /// "1 étape" / "3 étapes": picks the singular or plural key by the language's own rule.
    public static func plural(one: Key, other: Key, count: Int, language: Language? = nil) -> String {
        let language = language ?? current
        // French counts 0 and 1 as singular, English only 1.
        let singular = language == .fr ? (0 ... 1).contains(count) : count == 1
        return text(singular ? one : other, count, language: language)
    }

    /// "à l'instant", "il y a 5 min", "il y a 3 h", "il y a 2 j" (and the English forms).
    public static func relativeTime(from date: Date, to now: Date, language: Language? = nil) -> String {
        let language = language ?? current
        let seconds = max(0, now.timeIntervalSince(date))
        switch seconds {
        case ..<45: return text(.timeNow, language: language)
        case ..<3600: return text(.timeMinutes, max(1, Int((seconds / 60).rounded())), language: language)
        case ..<86400: return text(.timeHours, Int(seconds / 3600), language: language)
        default: return text(.timeDays, Int(seconds / 86400), language: language)
        }
    }

    /// "6 nov. 2026 à 10:42" / "Nov 6, 2026 at 10:42 AM".
    public static func dateTime(_ date: Date, language: Language? = nil, timeZone: TimeZone = .current) -> String {
        let language = language ?? current
        let formatter = DateFormatter()
        formatter.locale = language.locale
        formatter.timeZone = timeZone
        formatter.dateStyle = .medium
        formatter.timeStyle = .short
        return formatter.string(from: date)
    }

    static func table(_ language: Language) -> [Key: String] {
        language == .fr ? french : english
    }

    // MARK: - Français (tutoiement, court, un peu d'ironie)

    static let french: [Key: String] = [
        .menuOpen: "Ouvrir Pouple",
        .menuHideBubbles: "Masquer les bulles",
        .menuShowBubbles: "Afficher les bulles",
        .menuResetPosition: "Remettre les bulles à leur place",
        .menuStartAtLogin: "Lancer à l'ouverture de session",
        .menuQuit: "Quitter Pouple",
        .menuLanguage: "Langue / Language",
        .statusStarting: "Démarrage…",
        .statusUnavailable: "Moteur indisponible",
        .statusChecking: "Vérification…",
        .statusConnected: "Connecté",
        .statusSetupNeeded: "Configuration à terminer",
        .loginWaitingApproval: "à valider dans Réglages Système",
        .loginUnavailable: "indisponible pour cette version",
        .loginWithDetail: "%@ (%@)",
        .loginErrorTitle: "Pouple n'a pas pu modifier l'ouverture de session",
        .loginErrorBody: "%@\n\nÇa arrive surtout quand l'app n'est pas signée ou pas rangée dans Applications.",
        .languageChangeFailed: "Impossible de changer la langue : %@",
        .appAbout: "À propos de Pouple",
        .updateCheck: "Rechercher des mises à jour…",
        .updateChecking: "Recherche de mises à jour…",
        .updateAvailable: "Pouple %@ est disponible",
        .updateOpenInstructions: "Voir les instructions",
        .updateInstallInstructions: "Pour mettre Pouple à jour, relance la commande d’installation dans Terminal. Elle compile la nouvelle version sur ton Mac et remplace l’application installée.",
        .updateUpToDate: "Pouple est à jour",
        .updateInstalledVersion: "Version installée : %@",
        .updateUnavailable: "Mise à jour indisponible",
        .updateInvalidInstalledVersion: "Cette copie n’indique pas de version compatible avec la recherche de mises à jour.",
        .updateReleaseUnavailable: "Aucune version publique de Pouple n’est disponible sur GitHub.",
        .updateFailed: "La mise à jour n’a pas abouti",
        .updateTryAgain: "Impossible de vérifier les mises à jour sur GitHub. Vérifie ta connexion et réessaie dans un instant.",
        .updateBrowserFailed: "Les instructions d’installation n’ont pas pu être ouvertes dans ton navigateur.",
        .updateNow: "Mettre à jour maintenant",
        .updateNotFromSource: "Cette copie de Pouple n’a pas été installée depuis les sources : la mise à jour automatique n’est pas possible. Relance la commande d’installation dans Terminal.",
        .updateCheckoutMissing: "Le dossier des sources (%@) est introuvable. Relance la commande d’installation depuis une copie de pouple-app.",
        .updateTerminalFailed: "Terminal n’a pas pu s’ouvrir : %@",
        .scriptQuitting: "Fermeture de Pouple…",
        .scriptFetching: "Récupération du nouveau code…",
        .scriptBuilding: "Compilation et installation de Pouple (quelques minutes, garde cette fenêtre ouverte)…",
        .scriptDone: "Terminé : tes connexions et tes réglages sont conservés. Pouple se rouvre.",
        .scriptNotQuit: "Pouple ne s’est pas fermé. Quitte-le depuis la barre de menus, puis relance la mise à jour.",
        .scriptBadOrigin: "Ce dossier ne vient pas du dépôt Pouple : mise à jour annulée par sécurité.",
        .scriptPullFailed: "git pull a échoué (modifications locales ?). Corrige, puis lance : git pull --ff-only && bash scripts/install-local.sh",
        .scriptInstallFailed: "L’installation a échoué. Relance : bash scripts/install-local.sh",
        .scriptClose: "Appuie sur Entrée pour fermer cette fenêtre.",
        .appHide: "Masquer Pouple",
        .appHideOthers: "Masquer les autres",
        .appQuit: "Quitter Pouple",
        .editMenu: "Édition",
        .undo: "Annuler",
        .redo: "Rétablir",
        .cut: "Couper",
        .copy: "Copier",
        .paste: "Coller",
        .selectAll: "Tout sélectionner",
        .viewMenu: "Présentation",
        .reload: "Recharger",
        .windowMenu: "Fenêtre",
        .minimize: "Réduire",
        .close: "Fermer",
        .windowTitle: "Pouple",
        .placeholderStarting: "Pouple fait la sieste…",
        .placeholderLoading: "Encore un instant…",
        .placeholderFailedTitle: "Pouple n'a pas réussi à démarrer",
        .retry: "Réessayer",
        .dialogOK: "OK",
        .dialogCancel: "Annuler",
        .failStopped: "Le moteur s'est arrêté sans prévenir (code %@)",
        .failStatusUnknown: "inconnu",
        .failClosedBeforeReady: "Le moteur a fermé sa sortie avant d'être prêt",
        .failTimeoutOne: "Le moteur n'a pas répondu en %d seconde",
        .failTimeoutOther: "Le moteur n'a pas répondu en %d secondes",
        .failBinaryMissing: "Le moteur est introuvable : %@",
        .failReadyMalformed: "La première ligne du moteur est illisible",
        .failNotReady: "Le moteur n'a pas dit qu'il était prêt",
        .failInvalidPort: "Le moteur a annoncé un port inutilisable (%d)",
        .apiAnswered: "Pouple a répondu %d",
        .apiUnexpected: "Pouple a répondu de travers",
        .bubbleCollapse: "Réduire",
        .bubbleClose: "Fermer cette bulle",
        .handleWorking: "Enquête en cours…",
        .bubbleSomethingWrong: "Quelque chose a coincé",
        .bubbleRetry: "Enquêter à nouveau",
        .bubbleDone: "Terminé",
        .bubbleApply: "Faire la modification",
        .bubbleNotNow: "Pas maintenant",
        .bubbleOpen: "Ouvrir Pouple",
        .bubbleCopy: "Copier",
        .bubbleSuggestedReply: "Réponse suggérée",
        .bubbleApplyIn: "Appliquer cette modification dans %@ ?",
        .badgeApprove: "Modification à valider",
        .badgeNeedsAnswer: "À toi de décider",
        .badgeFailed: "Échec",
        .badgeApplied: "Modification faite",
        .badgeAnswerReady: "Réponse prête",
        .badgeWorking: "Travail en cours",
        .bubbleIgnore: "Ignorer",
        .bubbleStop: "Arrêter",
        .bubbleWorking: "Huit bras sur le coup.",
        .activityOne: "Activité · %d étape",
        .activityOther: "Activité · %d étapes",
        .handleName: "Pouple",
        .handleOverflow: "Pouple · +%d autres",
        .handleDrag: "Glisse pour déplacer",
        .handleOffline: "Hors ligne",
        .replyPlaceholder: "Une question de plus ?",
        .replySend: "Envoyer",
        .wizardOther: "Autre chose…",
        .wizardBack: "Retour",
        .wizardNext: "Suivant",
        .wizardSend: "Envoyer",
        .wizardSeveral: "Un ou plusieurs choix",
        .titleNoteToSelf: "Note à toi-même",
        .titleDirect: "%@ · message direct",
        .titleGroup: "%@ · discussion de groupe",
        .titleBroadcast: "%@ · @here dans %@",
        .titleKeyword: "%@ · mot surveillé dans %@",
        .titleThread: "%@ · fil dans %@",
        .titleChannel: "%@ · %@",
        .titleAction: "Action Pouple",
        .timeNow: "à l'instant",
        .timeMinutes: "il y a %d min",
        .timeHours: "il y a %d h",
        .timeDays: "il y a %d j",
    ]

    // MARK: - English

    static let english: [Key: String] = [
        .menuOpen: "Open Pouple",
        .menuHideBubbles: "Hide bubbles",
        .menuShowBubbles: "Show bubbles",
        .menuResetPosition: "Reset bubbles position",
        .menuStartAtLogin: "Start at login",
        .menuQuit: "Quit Pouple",
        .menuLanguage: "Langue / Language",
        .statusStarting: "Starting…",
        .statusUnavailable: "Core unavailable",
        .statusChecking: "Checking…",
        .statusConnected: "Connected",
        .statusSetupNeeded: "Setup needed",
        .loginWaitingApproval: "waiting for approval in System Settings",
        .loginUnavailable: "unavailable for this build",
        .loginWithDetail: "%@ (%@)",
        .loginErrorTitle: "Pouple could not change the login item",
        .loginErrorBody: "%@\n\nThis usually means the app is not signed or is not in the Applications folder.",
        .languageChangeFailed: "The language could not be changed: %@",
        .appAbout: "About Pouple",
        .updateCheck: "Check for Updates…",
        .updateChecking: "Checking for Updates…",
        .updateAvailable: "Pouple %@ is available",
        .updateOpenInstructions: "View Instructions",
        .updateInstallInstructions: "To update Pouple, rerun the install command in Terminal. It builds the new version on your Mac and replaces the installed app.",
        .updateUpToDate: "Pouple is up to date",
        .updateInstalledVersion: "Installed version: %@",
        .updateUnavailable: "Update unavailable",
        .updateInvalidInstalledVersion: "This copy does not report a version that can be checked for updates.",
        .updateReleaseUnavailable: "No public Pouple release is available on GitHub.",
        .updateFailed: "The update could not be completed",
        .updateTryAgain: "Could not check for updates on GitHub. Check your connection and try again shortly.",
        .updateBrowserFailed: "The installation instructions could not be opened in your browser.",
        .updateNow: "Update now",
        .updateNotFromSource: "This copy of Pouple was not installed from the sources, so it cannot update itself. Run the install command again in Terminal.",
        .updateCheckoutMissing: "The source folder (%@) could not be found. Run the install command again from a copy of pouple-app.",
        .updateTerminalFailed: "Terminal could not be opened: %@",
        .scriptQuitting: "Quitting Pouple…",
        .scriptFetching: "Fetching the new code…",
        .scriptBuilding: "Building and installing Pouple (a few minutes, keep this window open)…",
        .scriptDone: "Done: your connections and settings are kept. Pouple is reopening.",
        .scriptNotQuit: "Pouple did not quit. Quit it from the menu bar, then run the update again.",
        .scriptBadOrigin: "This folder does not come from the Pouple repository: update cancelled for safety.",
        .scriptPullFailed: "git pull failed (local changes?). Fix it, then run: git pull --ff-only && bash scripts/install-local.sh",
        .scriptInstallFailed: "The install failed. Run again: bash scripts/install-local.sh",
        .scriptClose: "Press Return to close this window.",
        .appHide: "Hide Pouple",
        .appHideOthers: "Hide Others",
        .appQuit: "Quit Pouple",
        .editMenu: "Edit",
        .undo: "Undo",
        .redo: "Redo",
        .cut: "Cut",
        .copy: "Copy",
        .paste: "Paste",
        .selectAll: "Select All",
        .viewMenu: "View",
        .reload: "Reload",
        .windowMenu: "Window",
        .minimize: "Minimize",
        .close: "Close",
        .windowTitle: "Pouple",
        .placeholderStarting: "Pouple is taking a nap…",
        .placeholderLoading: "Just a moment…",
        .placeholderFailedTitle: "Pouple could not start",
        .retry: "Retry",
        .dialogOK: "OK",
        .dialogCancel: "Cancel",
        .failStopped: "The core stopped unexpectedly (exit status %@)",
        .failStatusUnknown: "unknown",
        .failClosedBeforeReady: "The core closed its output before it was ready",
        .failTimeoutOne: "The core did not become ready within %d second",
        .failTimeoutOther: "The core did not become ready within %d seconds",
        .failBinaryMissing: "The core is missing at %@",
        .failReadyMalformed: "The core's first line of output is not valid",
        .failNotReady: "The core did not say it is ready",
        .failInvalidPort: "The core announced an unusable port (%d)",
        .apiAnswered: "Pouple answered %d",
        .apiUnexpected: "Pouple sent an unexpected response",
        .bubbleCollapse: "Collapse",
        .bubbleClose: "Close this bubble",
        .handleWorking: "Investigating…",
        .bubbleSomethingWrong: "Something went wrong",
        .bubbleRetry: "Investigate again",
        .bubbleDone: "Done",
        .bubbleApply: "Make the change",
        .bubbleNotNow: "Not now",
        .bubbleOpen: "Open Pouple",
        .bubbleCopy: "Copy",
        .bubbleSuggestedReply: "Suggested reply",
        .bubbleApplyIn: "Apply this change in %@?",
        .badgeApprove: "Approve a change",
        .badgeNeedsAnswer: "Your call",
        .badgeFailed: "Failed",
        .badgeApplied: "Change applied",
        .badgeAnswerReady: "Answer ready",
        .badgeWorking: "Working",
        .bubbleIgnore: "Ignore",
        .bubbleStop: "Stop",
        .bubbleWorking: "Eight arms on it.",
        .activityOne: "Activity · %d step",
        .activityOther: "Activity · %d steps",
        .handleName: "Pouple",
        .handleOverflow: "Pouple · +%d more",
        .handleDrag: "Drag to move",
        .handleOffline: "Offline",
        .replyPlaceholder: "Ask a follow-up…",
        .replySend: "Send",
        .wizardOther: "Something else…",
        .wizardBack: "Back",
        .wizardNext: "Next",
        .wizardSend: "Send",
        .wizardSeveral: "Pick one or more",
        .titleNoteToSelf: "Note to self",
        .titleDirect: "%@ · direct message",
        .titleGroup: "%@ · group chat",
        .titleBroadcast: "%@ · @here in %@",
        .titleKeyword: "%@ · watched word in %@",
        .titleThread: "%@ · thread in %@",
        .titleChannel: "%@ · %@",
        .titleAction: "Pouple action",
        .timeNow: "just now",
        .timeMinutes: "%d min ago",
        .timeHours: "%d h ago",
        .timeDays: "%d d ago",
    ]
}

/// French typography: a non-breaking space before `:`, a narrow one before `; ! ?`, and inside « ».
public enum French {
    static let nbsp = "\u{00A0}"
    static let narrow = "\u{202F}"

    public static func typographic(_ text: String) -> String {
        var result = text
        result = result.replacingOccurrences(of: " :", with: nbsp + ":")
        for mark in [";", "!", "?"] { result = result.replacingOccurrences(of: " " + mark, with: narrow + mark) }
        result = result.replacingOccurrences(of: "« ", with: "«" + nbsp)
        result = result.replacingOccurrences(of: " »", with: nbsp + "»")
        return result
    }
}
