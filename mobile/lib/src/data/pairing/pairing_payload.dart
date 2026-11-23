/// What the QR code on the Mac carries: where to reach it and the certificate to pin.
library;

/// The pairing text could not be understood.
class PairingPayloadException implements Exception {
  const PairingPayloadException(this.reason);

  /// A short, content-free reason shown to the user.
  final String reason;

  @override
  String toString() => 'PairingPayloadException: $reason';
}

final _hex64 = RegExp(r'^[0-9a-f]{64}$');
final _hex32 = RegExp(r'^[0-9a-f]{32}$');
final _hostChars = RegExp(r'^[A-Za-z0-9._:\-\[\]]+$');

/// The content of `pouple-pair:v1?hosts=...&port=...&fp=...&code=...&name=...`.
class PairingPayload {
  const PairingPayload({
    required this.hosts,
    required this.port,
    required this.fingerprint,
    required this.code,
    required this.macName,
  });

  /// Reads the pairing text of the QR code, or of a manual paste.
  factory PairingPayload.parse(String text) {
    final trimmed = text.trim();
    const prefix = 'pouple-pair:v1?';
    if (!trimmed.startsWith(prefix)) {
      throw const PairingPayloadException('This is not a Pouple pairing code.');
    }
    final Map<String, String> query;
    try {
      query = Uri.splitQueryString(trimmed.substring(prefix.length));
    } on FormatException {
      throw const PairingPayloadException('The pairing code is damaged.');
    }
    final hosts = <String>[];
    for (final raw in (query['hosts'] ?? '').split(',')) {
      final host = raw.trim();
      if (host.isEmpty) continue;
      if (!_hostChars.hasMatch(host)) {
        throw const PairingPayloadException('The pairing code has an invalid address.');
      }
      hosts.add(host.startsWith('[') && host.endsWith(']') ? host.substring(1, host.length - 1) : host);
    }
    if (hosts.isEmpty) {
      throw const PairingPayloadException('The pairing code has no address.');
    }
    final port = int.tryParse(query['port'] ?? '');
    if (port == null || port < 1 || port > 65535) {
      throw const PairingPayloadException('The pairing code has an invalid port.');
    }
    final fingerprint = (query['fp'] ?? '').toLowerCase();
    if (!_hex64.hasMatch(fingerprint)) {
      throw const PairingPayloadException('The pairing code has an invalid certificate fingerprint.');
    }
    final code = (query['code'] ?? '').toLowerCase();
    if (!_hex32.hasMatch(code)) {
      throw const PairingPayloadException('The pairing code is invalid.');
    }
    final name = (query['name'] ?? '').trim();
    return PairingPayload(
      hosts: List.unmodifiable(hosts),
      port: port,
      fingerprint: fingerprint,
      code: code,
      macName: name.isEmpty ? 'Mac' : name,
    );
  }

  /// Addresses to try in order (IP addresses, then the Mac's `.local` name). IPv6 without brackets.
  final List<String> hosts;
  final int port;

  /// SHA-256 of the certificate's DER, 64 lowercase hex characters.
  final String fingerprint;

  /// The single-use pairing code.
  final String code;

  /// The Mac's name, for display.
  final String macName;
}
