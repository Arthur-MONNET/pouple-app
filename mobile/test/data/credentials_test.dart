import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:pouple_companion/src/data/net/remote_client.dart';
import 'package:pouple_companion/src/data/storage/credentials.dart';

Map<String, Object?> _storedBeforeExternal() => {
  'macName': 'Studio',
  'hosts': ['192.168.1.20'],
  'port': 47821,
  'fingerprint': 'a' * 64,
  'deviceId': 'dev1',
  'token': 'secret',
};

void main() {
  test('a pairing stored before the external address existed still loads, with none', () {
    final credentials = Credentials.fromJson(jsonDecode(jsonEncode(_storedBeforeExternal())));
    expect(credentials.external, isNull);
    expect(credentials.endpoint.external, isNull);
    expect(credentials.endpoint.hosts, ['192.168.1.20']);
    expect(credentials.token, 'secret');
    expect(credentials.toJson().containsKey('external'), isFalse);
  });

  test('the external address survives a save and a load', () {
    final credentials = Credentials.fromJson(_storedBeforeExternal())
        .withExternal(ExternalAddress.parse('[2001:db8::1]:443'));
    final back = Credentials.fromJson(jsonDecode(jsonEncode(credentials.toJson())));
    expect(back.external, ExternalAddress.parse('[2001:db8::1]:443'));
    expect(back.endpoint.external, back.external);
  });

  test('a damaged stored external address counts as none instead of breaking the pairing', () {
    final json = _storedBeforeExternal()..['external'] = 'not an address';
    final credentials = Credentials.fromJson(json);
    expect(credentials.external, isNull);
    expect(credentials.token, 'secret');
  });

  test('withExternal changes the address and nothing else', () {
    final before = Credentials.fromJson(_storedBeforeExternal());
    final after = before.withExternal(ExternalAddress.parse('mac.example.org:8443'));
    expect(after.token, before.token);
    expect(after.deviceId, before.deviceId);
    expect(after.hosts, before.hosts);
    expect(after.port, before.port);
    expect(after.fingerprint, before.fingerprint);
    expect(after.macName, before.macName);
  });
}
