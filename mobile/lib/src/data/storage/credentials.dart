import 'dart:convert';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';

import '../models/json_reader.dart';
import '../net/remote_client.dart';

/// What pairing leaves on the phone: where the Mac is, the certificate to pin and the token.
class Credentials {
  const Credentials({
    required this.macName,
    required this.hosts,
    required this.port,
    required this.fingerprint,
    required this.deviceId,
    required this.token,
    this.external,
    this.tunnel,
  });

  factory Credentials.fromJson(Object? value) {
    final json = JsonObject.of(value);
    return Credentials(
      macName: json.stringOr('macName', 'Mac'),
      hosts: json.strings('hosts'),
      port: json.integer('port'),
      fingerprint: json.string('fingerprint'),
      deviceId: json.string('deviceId'),
      token: json.string('token'),
      // Pairings made before the external address existed have none; a damaged value counts as none.
      external: ExternalAddress.tryParse(json.optString('external')),
      // Same for the tunnel domain, which older pairings do not have.
      tunnel: parseTunnelDomain(_optText(json, 'tunnel')),
    );
  }

  final String macName;
  final List<String> hosts;
  final int port;
  final String fingerprint;
  final String deviceId;

  /// The bearer token. Never printed, never logged, never backed up.
  final String token;

  /// Where the Mac is reachable from outside the home network, when it is.
  final ExternalAddress? external;

  /// The ngrok domain the Mac is reachable through, when it has one.
  final String? tunnel;

  Endpoint get endpoint =>
      Endpoint(hosts: hosts, port: port, fingerprint: fingerprint, external: external, tunnel: tunnel);

  /// The same pairing with another external address: the token, the keys and the home addresses stay.
  Credentials withExternal(ExternalAddress address) => Credentials(
    macName: macName,
    hosts: hosts,
    port: port,
    fingerprint: fingerprint,
    deviceId: deviceId,
    token: token,
    external: address,
    tunnel: tunnel,
  );

  /// The same pairing with another tunnel domain: the token, the keys and the addresses stay.
  Credentials withTunnel(String domain) => Credentials(
    macName: macName,
    hosts: hosts,
    port: port,
    fingerprint: fingerprint,
    deviceId: deviceId,
    token: token,
    external: external,
    tunnel: domain,
  );

  Map<String, Object?> toJson() => {
    'macName': macName,
    'hosts': hosts,
    'port': port,
    'fingerprint': fingerprint,
    'deviceId': deviceId,
    'token': token,
    if (external != null) 'external': external.toString(),
    if (tunnel != null) 'tunnel': tunnel,
  };

  /// A description without the token.
  @override
  String toString() => 'Credentials(device: $deviceId, mac: $macName)';
}

String? _optText(JsonObject json, String key) {
  try {
    return json.optString(key);
  } on FormatException {
    return null;
  }
}

/// The one way to build the client of a paired phone, used by the foreground, the stream and the background check:
/// same candidate order, same route memory, same pinning.
RemoteClient createPairedClient(
  Credentials credentials, {
  Future<void> Function(ExternalAddress address)? onExternalChanged,
  Future<void> Function(String domain)? onTunnelChanged,
}) => RemoteClient(
  endpoint: credentials.endpoint,
  token: credentials.token,
  routeMemory: const PrefsRouteMemory(),
  onExternalChanged: onExternalChanged,
  onTunnelChanged: onTunnelChanged,
);

/// Stores a new external address in the saved pairing, and nothing else.
Future<void> saveExternal(CredentialStore store, ExternalAddress address) async {
  final current = await store.load();
  if (current == null || current.external == address) return;
  await store.save(current.withExternal(address));
}

/// Stores a new tunnel domain in the saved pairing, and nothing else.
Future<void> saveTunnel(CredentialStore store, String domain) async {
  final current = await store.load();
  if (current == null || current.tunnel == domain) return;
  await store.save(current.withTunnel(domain));
}

/// Where the credentials live.
abstract interface class CredentialStore {
  Future<Credentials?> load();
  Future<void> save(Credentials credentials);
  Future<void> clear();
}

/// The Android Keystore-backed secure storage.
class SecureCredentialStore implements CredentialStore {
  SecureCredentialStore([FlutterSecureStorage? storage]) : _storage = storage ?? const FlutterSecureStorage();

  static const _key = 'pouple.credentials.v1';
  final FlutterSecureStorage _storage;

  @override
  Future<Credentials?> load() async {
    final raw = await _storage.read(key: _key);
    if (raw == null) return null;
    try {
      return Credentials.fromJson(jsonDecode(raw));
    } on FormatException {
      return null;
    }
  }

  @override
  Future<void> save(Credentials credentials) =>
      _storage.write(key: _key, value: jsonEncode(credentials.toJson()));

  @override
  Future<void> clear() => _storage.delete(key: _key);
}
