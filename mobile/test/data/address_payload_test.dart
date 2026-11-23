import 'package:flutter_test/flutter_test.dart';
import 'package:pouple_companion/src/data/pairing/address_payload.dart';
import 'package:pouple_companion/src/data/pairing/pairing_payload.dart';

const _fp = '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef';

void main() {
  test('parses the address code', () {
    final payload = AddressPayload.parse('pouple-address:v1?ext=mac.example.org%3A8443&fp=$_fp');
    expect(payload.external.toString(), 'mac.example.org:8443');
    expect(payload.fingerprint, _fp);
  });

  test('reads an IPv6 literal, a raw colon and a fingerprint in capitals', () {
    final payload = AddressPayload.parse(
      '  pouple-address:v1?ext=[2001:db8::1]:443&fp=${_fp.toUpperCase()}\n',
    );
    expect(payload.external.host, '2001:db8::1');
    expect(payload.external.port, 443);
    expect(payload.fingerprint, _fp);
  });

  for (final entry in {
    'wrong scheme': 'pouple-pair:v1?ext=a.example:1&fp=$_fp',
    'wrong version': 'pouple-address:v2?ext=a.example:1&fp=$_fp',
    'no ext': 'pouple-address:v1?fp=$_fp',
    'empty ext': 'pouple-address:v1?ext=&fp=$_fp',
    'bad ext': 'pouple-address:v1?ext=a.example:0&fp=$_fp',
    'unbracketed IPv6': 'pouple-address:v1?ext=fd00::1:443&fp=$_fp',
    'no fingerprint': 'pouple-address:v1?ext=a.example:1',
    'short fingerprint': 'pouple-address:v1?ext=a.example:1&fp=abcd',
    'non-hex fingerprint': 'pouple-address:v1?ext=a.example:1&fp=${'z' * 64}',
  }.entries) {
    test('refuses ${entry.key}', () {
      expect(() => AddressPayload.parse(entry.value), throwsA(isA<PairingPayloadException>()));
    });
  }

  test('tells the two kinds of code apart', () {
    expect(payloadKind('pouple-pair:v1?x'), PayloadKind.pairing);
    expect(payloadKind('  pouple-address:v1?x'), PayloadKind.address);
    expect(payloadKind('https://example.org'), PayloadKind.unknown);
  });
}
