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
    );
  }

  final String macName;
  final List<String> hosts;
  final int port;
  final String fingerprint;
  final String deviceId;

  /// The bearer token. Never printed, never logged, never backed up.
  final String token;

  Endpoint get endpoint => Endpoint(hosts: hosts, port: port, fingerprint: fingerprint);

  Map<String, Object?> toJson() => {
    'macName': macName,
    'hosts': hosts,
    'port': port,
    'fingerprint': fingerprint,
    'deviceId': deviceId,
    'token': token,
  };

  /// A description without the token.
  @override
  String toString() => 'Credentials(device: $deviceId, mac: $macName)';
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
