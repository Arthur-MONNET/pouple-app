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

/// TLS over the socket of [task], with the same pin as [createPinnedHttpClient]: no certificate authority, no host
/// name, only the certificate whose SHA-256 is [pinnedHex]. For an HTTP client whose `connectionFactory` supplies
/// the socket: the client then applies neither its security context nor its certificate callback, so the handshake
/// is done here. [onRefused] is called when another certificate was presented. The TLS peer is not named (no SNI).
ConnectionTask<Socket> pinnedSecureTask(
  ConnectionTask<Socket> task,
  String pinnedHex, {
  void Function()? onRefused,
}) {
  final secured = task.socket.then<Socket>(
    (socket) => SecureSocket.secure(
      socket,
      host: InternetAddress.loopbackIPv4,
      context: SecurityContext(withTrustedRoots: false),
      onBadCertificate: (certificate) {
        if (fingerprintMatches(certificate.der, pinnedHex)) return true;
        onRefused?.call();
        return false;
      },
    ),
  );
  return ConnectionTask.fromSocket(secured, task.cancel);
}

/// An HTTP client that trusts no certificate authority and no host name: it accepts the one certificate whose
/// SHA-256 is [pinnedHex], and nothing else. There is no fallback to an unpinned connection.
///
/// [onRefused] is told, from inside the certificate callback, which `host`/`port` presented another certificate.
/// That is the only way to know a TLS error is a pin failure: a handshake that is cut by the peer is also a
/// `TlsException` but never reaches this callback.
HttpClient createPinnedHttpClient(String pinnedHex, {void Function(String host, int port)? onRefused}) {
  final client = HttpClient(context: SecurityContext(withTrustedRoots: false));
  client.badCertificateCallback = (certificate, host, port) {
    if (fingerprintMatches(certificate.der, pinnedHex)) return true;
    onRefused?.call(host, port);
    return false;
  };
  client.connectionTimeout = const Duration(seconds: 10);
  client.idleTimeout = const Duration(seconds: 30);
  return client;
}
