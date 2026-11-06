import Foundation

/// What the in-app "Update" button runs in Terminal: quit Pouple, pull the code, rebuild and reopen. The app only writes
/// this file and opens it; the file does the work in a window the user can watch.
public enum UpdateScript {
    /// The record `scripts/install-local.sh` leaves in Pouple's data folder.
    public struct InstallRecord: Decodable, Equatable, Sendable {
        public let sourceRoot: String
        public let commit: String?
        public let version: String?
    }

    public enum Problem: Error, Equatable, Sendable {
        case notInstalledFromSource
        case invalidRecord
        case checkoutMissing(String)
    }

    /// Reads and checks the record. `fileExists` is injected so the rules can be tested without a disk.
    public static func plan(recordData: Data?, directoryExists: (String) -> Bool, fileExists: (String) -> Bool) -> Result<InstallRecord, Problem> {
        guard let recordData else { return .failure(.notInstalledFromSource) }
        guard let record = try? JSONDecoder().decode(InstallRecord.self, from: recordData), isPlainAbsolutePath(record.sourceRoot) else {
            return .failure(.invalidRecord)
        }
        guard directoryExists(record.sourceRoot), fileExists(record.sourceRoot + "/scripts/install-local.sh") else {
            return .failure(.checkoutMissing(record.sourceRoot))
        }
        return .success(record)
    }

    /// An absolute path made of ordinary characters: no newline, no NUL, nothing that could change the meaning of a line.
    static func isPlainAbsolutePath(_ path: String) -> Bool {
        guard path.hasPrefix("/"), path.count < 1024, !path.contains("/../"), !path.hasSuffix("/..") else { return false }
        return !path.unicodeScalars.contains { $0.value < 0x20 || $0.value == 0x7F }
    }

    /// Single-quotes a text for a POSIX shell: the only character that needs care is the quote itself.
    public static func shellQuote(_ text: String) -> String {
        "'" + text.replacingOccurrences(of: "'", with: "'\\''") + "'"
    }

    /// The texts the script shows, in the user's language.
    public struct Messages: Sendable {
        public let quitting, fetching, building, done, notQuit, badOrigin, pullFailed, installFailed, close: String
        public init(quitting: String, fetching: String, building: String, done: String, notQuit: String, badOrigin: String, pullFailed: String, installFailed: String, close: String) {
            self.quitting = quitting; self.fetching = fetching; self.building = building; self.done = done
            self.notQuit = notQuit; self.badOrigin = badOrigin; self.pullFailed = pullFailed; self.installFailed = installFailed; self.close = close
        }
    }

    /// The accepted remotes: the project's own repository, and nothing else. A forged record cannot point the updater
    /// at somebody else's code.
    static let allowedOrigins = [
        "https://github.com/Arthur-MONNET/pouple-app", "https://github.com/Arthur-MONNET/pouple-app.git",
        "git@github.com:Arthur-MONNET/pouple-app", "git@github.com:Arthur-MONNET/pouple-app.git",
    ]

    /// The text of the `.command` file. Only two values are interpolated, both quoted or numeric: the checkout path
    /// and the process id of the app to wait for.
    public static func content(sourceRoot: String, appPID: Int32, messages m: Messages) -> String {
        let origins = allowedOrigins.map(shellQuote).joined(separator: "|")
        return """
        #!/bin/bash
        # Pouple update, written by the app for this one run. Read it: it is short.
        set -u
        ROOT=\(shellQuote(sourceRoot))
        APP_PID=\(appPID)

        say() { printf '\\n==> %s\\n' "$1"; }
        fail() {
          printf '\\n[x] %s\\n' "$1"
          printf '%s ' \(shellQuote(m.close))
          read -r _
          exit 1
        }

        say \(shellQuote(m.quitting))
        for _ in $(seq 1 80); do
          kill -0 "$APP_PID" 2>/dev/null || break
          sleep 0.5
        done
        if kill -0 "$APP_PID" 2>/dev/null; then fail \(shellQuote(m.notQuit)); fi

        cd "$ROOT" || fail \(shellQuote(m.pullFailed))
        origin="$(git remote get-url origin 2>/dev/null || true)"
        case "$origin" in
          \(origins)) ;;
          *) fail \(shellQuote(m.badOrigin)) ;;
        esac

        say \(shellQuote(m.fetching))
        GIT_TERMINAL_PROMPT=0 git pull --ff-only origin main || fail \(shellQuote(m.pullFailed))

        say \(shellQuote(m.building))
        bash scripts/install-local.sh || fail \(shellQuote(m.installFailed))

        say \(shellQuote(m.done))
        # This file was written for this one run: remove it (and its private folder) once everything went well.
        rm -f -- "$0"; rmdir -- "$(dirname -- "$0")" 2>/dev/null || true
        sleep 2
        exit 0

        """
    }
}
