import 'package:flutter_test/flutter_test.dart';
import 'package:pouple_companion/src/data/pairing/pairing_payload.dart';

const _fp = '0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef';
const _code = 'fedcba9876543210fedcba9876543210';

String _payload({
  String hosts = '192.168.1.20%2Cmymac.local',
  String port = '47821',
  String fp = _fp,
  String code = _code,
  String name = 'My+Mac',
}) => 'pouple-pair:v1?hosts=$hosts&port=$port&fp=$fp&code=$code&name=$name';

void main() {
  test('parses the payload the core writes', () {
    final payload = PairingPayload.parse(_payload());
    expect(payload.hosts, ['192.168.1.20', 'mymac.local']);
    expect(payload.port, 47821);
    expect(payload.fingerprint, _fp);
    expect(payload.code, _code);
    expect(payload.macName, 'My Mac');
  });

  test('keeps IPv6 addresses without brackets and tolerates surrounding whitespace', () {
    final payload = PairingPayload.parse('  ${_payload(hosts: 'fd00%3A%3A1%2C%5Bfd00%3A%3A2%5D')}\n');
    expect(payload.hosts, ['fd00::1', 'fd00::2']);
  });

  test('lowercases the fingerprint and code', () {
    final payload = PairingPayload.parse(_payload(fp: _fp.toUpperCase(), code: _code.toUpperCase()));
    expect(payload.fingerprint, _fp);
    expect(payload.code, _code);
  });

  test('a missing name defaults to Mac', () {
    expect(PairingPayload.parse(_payload(name: '')).macName, 'Mac');
  });

  for (final entry in {
    'wrong scheme': 'https://example.com',
    'wrong version': 'pouple-pair:v2?hosts=a&port=1',
    'no hosts': _payload(hosts: ''),
    'bad host characters': _payload(hosts: 'a%2Fb'),
    'port zero': _payload(port: '0'),
    'port too large': _payload(port: '70000'),
    'port not a number': _payload(port: 'abc'),
    'short fingerprint': _payload(fp: 'abcd'),
    'non-hex fingerprint': _payload(fp: 'z' * 64),
    'short code': _payload(code: 'abcd'),
  }.entries) {
    test('refuses ${entry.key}', () {
      expect(() => PairingPayload.parse(entry.value), throwsA(isA<PairingPayloadException>()));
    });
  }
}
