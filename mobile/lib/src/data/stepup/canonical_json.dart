import 'dart:convert';
import 'dart:typed_data';

import 'package:crypto/crypto.dart';

/// A step-up value the Mac would refuse: the canonical form never needs JSON escaping, so anything that would is
/// rejected here, before the person is asked for a fingerprint.
class StepUpFormatException implements Exception {
  const StepUpFormatException(this.field);

  final String field;

  @override
  String toString() => 'StepUpFormatException: $field';
}

/// The routes a step-up can name, exactly as the Mac's allowlist spells them (`{id}` stays as it is).
abstract final class StepUpRoute {
  static const plan = 'POST /api/assistant/requests/{id}/plan';
  static const proposal = 'POST /api/assistant/requests/{id}/proposal';
  static const messages = 'POST /api/assistant/requests/{id}/messages';
  static const answers = 'POST /api/assistant/requests/{id}/answers';
  static const newConversation = 'POST /api/assistant/requests';
  static const remove = 'DELETE /api/assistant/requests/{id}';
}

/// SHA-256 of the exact body bytes, in lowercase hex (an empty body gives the well-known empty digest).
String bodySha256(List<int> body) => sha256.convert(body).toString();

bool _validValue(String value) {
  if (value.length > 128) return false;
  for (final unit in value.codeUnits) {
    final ok =
        (unit >= 0x61 && unit <= 0x7a) || // a-z
        (unit >= 0x41 && unit <= 0x5a) || // A-Z
        (unit >= 0x30 && unit <= 0x39) || // 0-9
        unit == 0x2e || // .
        unit == 0x5f || // _
        unit == 0x3a || // :
        unit == 0x2d; // -
    if (!ok) return false;
  }
  return true;
}

bool _validRoute(String route) {
  if (route.isEmpty || route.length > 128) return false;
  for (final unit in route.codeUnits) {
    final ok =
        (unit >= 0x61 && unit <= 0x7a) ||
        (unit >= 0x41 && unit <= 0x5a) ||
        (unit >= 0x30 && unit <= 0x39) ||
        unit == 0x2f || // /
        unit == 0x7b || // {
        unit == 0x7d || // }
        unit == 0x20 || // space
        unit == 0x2d; // -
    if (!ok) return false;
  }
  return true;
}

/// The bytes the Keystore key signs: the canonical JSON of docs/REMOTE-API.md, "Step-up".
///
/// All keys are present, in this order, with no whitespace. [actions] is the body's `actions` array, or null when the
/// body has none (the Mac encodes that as `null`). The Mac verifies the signature over exactly these bytes, so this
/// must stay byte-for-byte what core/internal/remote/stepup.go's `Canonical` writes.
String canonicalStepUp({
  required String nonce,
  required String deviceId,
  required String route,
  required String requestId,
  required String planId,
  required String proposalId,
  required List<int>? actions,
  required String bodySha256,
}) {
  final values = {
    'nonce': nonce,
    'deviceId': deviceId,
    'requestId': requestId,
    'planId': planId,
    'proposalId': proposalId,
    'bodySha256': bodySha256,
  };
  for (final entry in values.entries) {
    if (!_validValue(entry.value)) throw StepUpFormatException(entry.key);
  }
  if (!_validRoute(route)) throw const StepUpFormatException('route');
  final actionsJson = actions == null ? 'null' : '[${actions.join(',')}]';
  return '{"v":1,"nonce":"$nonce","deviceId":"$deviceId","route":"$route","requestId":"$requestId",'
      '"planId":"$planId","proposalId":"$proposalId","actions":$actionsJson,"bodySha256":"$bodySha256"}';
}

/// UTF-8 bytes of [canonicalStepUp]'s result, as signed.
Uint8List canonicalBytes(String canonical) => Uint8List.fromList(utf8.encode(canonical));
