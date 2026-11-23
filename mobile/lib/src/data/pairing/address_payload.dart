/// The secret-free text the Mac shows when its public address changed: `pouple-address:v1?ext=...&fp=...`.
library;

import '../net/external_address.dart';
import 'pairing_payload.dart';

/// What a scanned or pasted text is.
enum PayloadKind { pairing, address, unknown }

PayloadKind payloadKind(String text) {
  final trimmed = text.trim();
  if (trimmed.startsWith('pouple-pair:')) return PayloadKind.pairing;
  if (trimmed.startsWith('pouple-address:')) return PayloadKind.address;
  return PayloadKind.unknown;
}

final _hex64 = RegExp(r'^[0-9a-f]{64}$');

/// A new external address for an already paired Mac. It carries no token and no code.
class AddressPayload {
  const AddressPayload({required this.external, required this.fingerprint});

  factory AddressPayload.parse(String text) {
    final trimmed = text.trim();
    const prefix = 'pouple-address:v1?';
    if (!trimmed.startsWith(prefix)) {
      throw const PairingPayloadException('This is not a Pouple address code.');
    }
    final Map<String, String> query;
    try {
      query = Uri.splitQueryString(trimmed.substring(prefix.length));
    } on FormatException {
      throw const PairingPayloadException('The address code is damaged.');
    }
    final ExternalAddress external;
    try {
      external = ExternalAddress.parse(query['ext'] ?? '');
    } on FormatException {
      throw const PairingPayloadException('The address code has an invalid address.');
    }
    final fingerprint = (query['fp'] ?? '').toLowerCase();
    if (!_hex64.hasMatch(fingerprint)) {
      throw const PairingPayloadException('The address code has an invalid certificate fingerprint.');
    }
    return AddressPayload(external: external, fingerprint: fingerprint);
  }

  final ExternalAddress external;

  /// SHA-256 of the Mac's certificate, lowercase hex.
  final String fingerprint;
}
