import 'package:flutter/foundation.dart';

import '../data/net/cert_pinning.dart';
import '../data/net/remote_client.dart';
import '../data/net/remote_failure.dart';
import '../data/pairing/address_payload.dart';
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
    RemoteClient Function(Endpoint endpoint, String? token)? probeFactory,
  }) : _probeFactory = probeFactory ?? ((endpoint, token) => RemoteClient(endpoint: endpoint, token: token)),
       _clientFactory =
           clientFactory ??
           ((endpoint, token) =>
               RemoteClient(endpoint: endpoint, token: token, routeMemory: const PrefsRouteMemory()));

  final CredentialStore store;
  final StepUpSigner signer;
  final RemoteClient Function(Endpoint endpoint, String? token) _clientFactory;

  /// Builds the throwaway clients that check a new address (no route memory, nothing learned).
  final RemoteClient Function(Endpoint endpoint, String? token) _probeFactory;

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
      Endpoint(
        hosts: payload.hosts,
        port: payload.port,
        fingerprint: payload.fingerprint,
        external: payload.external,
        tunnel: payload.tunnel,
      ),
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
        external: payload.external,
        tunnel: payload.tunnel,
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
      client.dispose();
    }
  }

  /// Keeps a new external address (the Mac announced it, or an address code carried it). Only the address changes.
  Future<void> updateExternal(ExternalAddress? address) async {
    final current = _credentials;
    if (current == null || current.external == address) return;
    final updated = current.withExternal(address);
    await store.save(updated);
    _credentials = updated;
    notifyListeners();
  }

  /// Keeps a new tunnel domain (the Mac announced it, or an address code carried it). Only the domain changes.
  Future<void> updateTunnel(String? domain) async {
    final current = _credentials;
    if (current == null || current.tunnel == domain) return;
    final updated = current.withTunnel(domain);
    await store.save(updated);
    _credentials = updated;
    notifyListeners();
  }

  /// Applies an address update code. It is accepted only when it carries the fingerprint of the paired Mac.
  /// Throws an [AddressUpdateException] otherwise.
  Future<void> applyAddress(AddressPayload payload) async {
    final current = _credentials;
    if (current == null) {
      throw const AddressUpdateException(
        'Pair this phone with the Mac first: an address code only updates a pairing.',
      );
    }
    if (!hexEqual(payload.fingerprint, current.fingerprint)) {
      throw const AddressUpdateException(
        'This address code is from another Mac (its certificate is different). Nothing was changed.',
      );
    }
    // The documented check: the new address must answer, with the pinned certificate, an authenticated request. A
    // code that is wrong, outdated or forged is refused before anything is stored.
    final tunnel = payload.tunnel;
    await _verify(
      current,
      Endpoint(hosts: const [], port: 0, fingerprint: current.fingerprint, external: payload.external),
    );
    if (tunnel != null) {
      await _verify(
        current,
        Endpoint(hosts: const [], port: 0, fingerprint: current.fingerprint, tunnel: tunnel),
      );
    }
    await updateExternal(payload.external);
    if (tunnel != null) await updateTunnel(tunnel);
  }

  Future<void> _verify(Credentials current, Endpoint only) async {
    final probe = _probeFactory(only, current.token);
    try {
      await probe.verify();
    } on RemoteFailure {
      throw const AddressUpdateException(
        'The Mac did not answer at this new address with its own certificate, so nothing was changed. '
        'Check that the address is reachable, then scan the code again.',
      );
    } finally {
      probe.dispose();
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
        client.dispose();
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

/// An address update code that was not applied, with a reason that can be shown.
class AddressUpdateException implements Exception {
  const AddressUpdateException(this.reason);

  final String reason;

  @override
  String toString() => 'AddressUpdateException: $reason';
}
