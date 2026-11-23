# Pouple companion (Android)

The phone app of Pouple. It pairs with the Mac over the local network and lets you follow and answer assistant
conversations. The contract with the Mac is [docs/REMOTE-API.md](../docs/REMOTE-API.md).

- Flutter 3.47.7 through FVM (`.fvmrc`). Use `fvm flutter ...`.
- Package `app.pouple.companion`, Android 8 (API 26) and later.

## Layout

```text
lib/src/data     models (strict JSON), pinned TLS client, SSE, pairing payload, secure storage, step-up
lib/src/domain   controllers: session, live conversations, actions, decisions, app lock
lib/src/ui       screens and widgets
lib/src/notifications  local notifications and the 15-minute background check
android/app/src/main/kotlin/app/pouple/companion  Keystore key and BiometricPrompt (platform channel), FLAG_SECURE
```

## Security

- TLS: no certificate authority and no host name check. The app pins the SHA-256 of the certificate DER that the QR
  code carries, and refuses any other certificate.
- The token lives in `flutter_secure_storage`. Backups are off (`allowBackup=false`, empty data extraction rules).
- Approving a plan or a proposal, deleting a conversation, and writing in a conversation that holds write grants need a
  fresh signature by an Android Keystore P-256 key that requires a strong biometric for each use. The signed bytes are
  the canonical JSON of the contract (`lib/src/data/stepup/canonical_json.dart`, tested against vectors produced by the
  Go verifier).
- `FLAG_SECURE` hides the app from screenshots and the recent-apps view (so `adb screencap` returns a black image).
- Notifications are local and never contain a conversation's text.

## Commands

```sh
fvm flutter analyze
fvm flutter test
fvm flutter build apk --release --split-per-abi
adb -s <serial> install -r build/app/outputs/flutter-apk/app-arm64-v8a-release.apk
```

Release signing reads `android/key.properties` (ignored by git): `storeFile`, `storePassword`, `keyAlias`,
`keyPassword`. Keep the keystore outside the repository. Without that file the release build is signed with the debug
key. For development over USB: `adb reverse tcp:47821 tcp:47821` and pair with host `127.0.0.1`.
