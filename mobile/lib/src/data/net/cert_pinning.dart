import 'dart:io';
import 'dart:typed_data';

import 'package:crypto/crypto.dart';

/// SHA-256 of a certificate's DER, in lowercase hex: what the QR code carries.
String certificateFingerprint(Uint8List der) => sha256.convert(der).toString();

/// Whether [der] is the pinned certificate. The comparison does not stop at the first different character.
bool fingerprintMatches(Uint8List der, String pinnedHex) => hexEqual(certificateFingerprint(der), pinnedHex);

/// Case-insensitive comparison of two hex strings that does not stop at the first different character.
bool hexEqual(String a, String b) {
  final actual = a.toLowerCase();
  final expected = b.toLowerCase();
  if (actual.length != expected.length) return false;
  var difference = 0;
  for (var i = 0; i < actual.length; i++) {
    difference |= actual.codeUnitAt(i) ^ expected.codeUnitAt(i);
  }
  return difference == 0;
}

/// An HTTP client that trusts no certificate authority and no host name: it accepts the one certificate whose
/// SHA-256 is [pinnedHex], and nothing else. There is no fallback to an unpinned connection.
HttpClient createPinnedHttpClient(String pinnedHex) {
  final client = HttpClient(context: SecurityContext(withTrustedRoots: false));
  client.badCertificateCallback = (certificate, host, port) => fingerprintMatches(certificate.der, pinnedHex);
  client.connectionTimeout = const Duration(seconds: 10);
  client.idleTimeout = const Duration(seconds: 30);
  return client;
}
