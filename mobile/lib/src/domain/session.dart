import 'package:flutter/foundation.dart';

import '../data/net/remote_client.dart';
import '../data/net/remote_failure.dart';
import '../data/pairing/pairing_payload.dart';
import '../data/stepup/step_up_signer.dart';
import '../data/storage/credentials.dart';

/// Where the app stands with the Mac.
enum SessionPhase { loading, unpaired, paired }

/// Pairing and unpairing, and the credentials they leave.
class SessionController extends ChangeNotifier {
  SessionController({
    required this.store,
    required this.signer,
    RemoteClient Function(Endpoint endpoint, String? token)? clientFactory,
  }) : _clientFactory =
           clientFactory ?? ((endpoint, token) => RemoteClient(endpoint: endpoint, token: token));

  final CredentialStore store;
  final StepUpSigner signer;
  final RemoteClient Function(Endpoint endpoint, String? token) _clientFactory;

  SessionPhase _phase = SessionPhase.loading;
  Credentials? _credentials;
  String? _notice;

  SessionPhase get phase => _phase;
  Credentials? get credentials => _credentials;

  /// A one-time message for the pairing screen (why the phone was unpaired).
  String? get notice => _notice;

  /// Reads the stored credentials.
  Future<void> restore() async {
    _credentials = await store.load();
    _phase = _credentials == null ? SessionPhase.unpaired : SessionPhase.paired;
    notifyListeners();
  }

  /// Pairs with the Mac described by [payload]: creates the key, sends the code and the public key, and stores the
  /// token. Throws a [RemoteFailure] or a [SignerException].
  Future<void> pair(PairingPayload payload) async {
    final publicKey = await signer.createKey();
    final client = _clientFactory(
      Endpoint(hosts: payload.hosts, port: payload.port, fingerprint: payload.fingerprint),
      null,
    );
    try {
      final name = sanitizeDeviceName(await signer.deviceName());
      final paired = await client.pair(code: payload.code, deviceName: name, stepUpPublicKey: publicKey);
      final credentials = Credentials(
        macName: payload.macName,
        hosts: payload.hosts,
        port: payload.port,
        fingerprint: payload.fingerprint,
        deviceId: paired.deviceId,
        token: paired.token,
      );
      await store.save(credentials);
      _credentials = credentials;
      _notice = null;
      _phase = SessionPhase.paired;
      notifyListeners();
    } on Object {
      await signer.deleteKey();
      rethrow;
    } finally {
      client.close();
    }
  }

  /// Unpairs: tells the Mac when it can be reached, then forgets the token and the key either way.
  Future<void> unpair() async {
    final credentials = _credentials;
    if (credentials != null) {
      final client = _clientFactory(credentials.endpoint, credentials.token);
      try {
        await client.unpairSelf();
      } on RemoteFailure {
        // The Mac may be away: the token is dropped here, and can be revoked on the Mac later.
      } finally {
        client.close();
      }
    }
    await forget(null);
  }

  /// Drops the credentials and the key. [notice] explains why when the Mac did it.
  Future<void> forget(String? notice) async {
    await store.clear();
    await signer.deleteKey();
    _credentials = null;
    _notice = notice;
    _phase = SessionPhase.unpaired;
    notifyListeners();
  }

  /// The Mac no longer knows this phone's token.
  Future<void> revoked() => forget('This phone was unpaired from the Mac. Pair it again.');
}

/// A device name the Mac accepts: 1 to 64 characters, letters, digits, punctuation and spaces.
String sanitizeDeviceName(String name) {
  final cleaned = name.replaceAll(RegExp(r'[^\p{L}\p{M}\p{N}\p{P} ]', unicode: true), '').trim();
  if (cleaned.isEmpty) return 'Android phone';
  return cleaned.length > 64 ? cleaned.substring(0, 64).trim() : cleaned;
}
