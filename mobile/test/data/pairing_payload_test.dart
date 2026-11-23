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
  String? ext,
}) => 'pouple-pair:v1?hosts=$hosts&port=$port&fp=$fp&code=$code&name=$name${ext == null ? '' : '&ext=$ext'}';

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

  group('external endpoint', () {
    test('is absent when the code has none', () {
      expect(PairingPayload.parse(_payload()).external, isNull);
    });

    test('reads a DNS name, an IPv4 address and a bracketed IPv6 literal, each with its own port', () {
      final dns = PairingPayload.parse(_payload(ext: 'Mac.Example.org%3A8443')).external!;
      expect((dns.host, dns.port), ('mac.example.org', 8443));
      final v4 = PairingPayload.parse(_payload(ext: '203.0.113.7:51000')).external!;
      expect((v4.host, v4.port), ('203.0.113.7', 51000));
      final v6 = PairingPayload.parse(_payload(ext: '%5B2001%3Adb8%3A%3A1%5D%3A443')).external!;
      expect((v6.host, v6.port), ('2001:db8::1', 443));
      expect(v6.toString(), '[2001:db8::1]:443');
    });

    test('keeps the other fields unchanged', () {
      final payload = PairingPayload.parse(_payload(ext: 'mac.example.org:8443'));
      expect(payload.hosts, ['192.168.1.20', 'mymac.local']);
      expect(payload.port, 47821);
      expect(payload.macName, 'My Mac');
    });

    test('accepts the port edges 1 and 65535', () {
      expect(PairingPayload.parse(_payload(ext: 'a.example:1')).external!.port, 1);
      expect(PairingPayload.parse(_payload(ext: 'a.example:65535')).external!.port, 65535);
    });

    for (final entry in {
      'empty': '',
      'no port': 'mac.example.org',
      'empty port': 'mac.example.org:',
      'port zero': 'mac.example.org:0',
      'port too large': 'mac.example.org:65536',
      'port with sign': 'mac.example.org:%2B80',
      'port with leading zero': 'mac.example.org:080',
      'port not a number': 'mac.example.org:http',
      'no host': ':8443',
      'bare IPv6': 'fd00%3A%3A1%3A8443',
      'IPv6 without port': '%5Bfd00%3A%3A1%5D',
      'IPv6 unclosed': '%5Bfd00%3A%3A1%3A8443',
      'invalid IPv6': '%5Bnot%3Aipv6%5D%3A443',
      'IPv6 with a zone': '%5Bfe80%3A%3A1%25en0%5D%3A443',
      'IPv4 out of range': '300.1.1.1:443',
      'IPv4 with five parts': '1.2.3.4.5:443',
      'IPv4 with a leading zero': '01.2.3.4:443',
      'host starting with a hyphen': '-mac.example.org:443',
      'host ending with a hyphen': 'mac-.example.org:443',
      'empty label': 'mac..example.org:443',
      'trailing dot': 'mac.example.org.:443',
      'space in host': 'mac%20x.example.org:443',
      'path in host': 'mac.example.org%2Fx:443',
      'userinfo': 'user%40mac.example.org:443',
      'scheme': 'https%3A%2F%2Fmac.example.org:443',
    }.entries) {
      test('refuses ${entry.key}', () {
        expect(
          () => PairingPayload.parse(_payload(ext: entry.value)),
          throwsA(isA<PairingPayloadException>()),
          reason: entry.value,
        );
      });
    }
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
