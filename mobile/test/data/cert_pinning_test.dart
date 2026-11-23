import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:pouple_companion/src/data/net/cert_pinning.dart';

void main() {
  final der = Uint8List.fromList(utf8.encode('abc'));
  // SHA-256("abc"), the standard test vector.
  const abc = 'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad';

  test('the fingerprint is the lowercase hex SHA-256 of the DER', () {
    expect(certificateFingerprint(der), abc);
  });

  test('accepts the pinned certificate, whatever the case of the pin', () {
    expect(fingerprintMatches(der, abc), isTrue);
    expect(fingerprintMatches(der, abc.toUpperCase()), isTrue);
  });

  test('refuses any other certificate', () {
    expect(fingerprintMatches(Uint8List.fromList(utf8.encode('abd')), abc), isFalse);
  });

  test('refuses a pin of another length or an empty pin', () {
    expect(fingerprintMatches(der, abc.substring(0, 63)), isFalse);
    expect(fingerprintMatches(der, '$abc$abc'), isFalse);
    expect(fingerprintMatches(der, ''), isFalse);
  });

  test('a pin differing by one character is refused', () {
    final flipped = '${abc.substring(0, 63)}${abc[63] == 'a' ? 'b' : 'a'}';
    expect(fingerprintMatches(der, flipped), isFalse);
  });
}
