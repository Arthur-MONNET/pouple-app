import 'package:flutter_test/flutter_test.dart';
import 'package:pouple_companion/src/data/net/remote_failure.dart';
import 'package:pouple_companion/src/data/net/remote_client.dart';
import 'package:pouple_companion/src/data/pairing/address_payload.dart';
import 'package:pouple_companion/src/data/pairing/pairing_payload.dart';
import 'package:pouple_companion/src/data/storage/credentials.dart';
import 'package:pouple_companion/src/domain/session.dart';

import '../support/fake_mac.dart';

class MemoryStore implements CredentialStore {
  Credentials? saved;

  @override
  Future<Credentials?> load() async => saved;

  @override
  Future<void> save(Credentials credentials) async => saved = credentials;

  @override
  Future<void> clear() async => saved = null;
}

void main() {
  late FakeMac mac;
  late MemoryStore store;
  late FakeSigner signer;
  late SessionController session;

  setUp(() async {
    mac = await FakeMac.start();
    store = MemoryStore();
    signer = FakeSigner();
    session = SessionController(
      store: store,
      signer: signer,
      clientFactory: (endpoint, token) => mac.client(token: token),
    );
  });
  tearDown(() => mac.close());

  PairingPayload payload() => PairingPayload.parse(
    'pouple-pair:v1?hosts=127.0.0.1&port=${mac.port}&fp=${'a' * 64}&code=${'b' * 32}&name=Studio',
  );

  test('restores to unpaired when nothing is stored', () async {
    await session.restore();
    expect(session.phase, SessionPhase.unpaired);
  });

  test('pairing creates the key, sends the code and public key, and stores the token', () async {
    mac.answers['POST /remote/v1/pair'] = (201, {'deviceId': 'dev1', 'token': 'secret-token'});
    await session.pair(payload());
    expect(session.phase, SessionPhase.paired);
    expect(signer.keyCreated, isTrue);
    expect(store.saved!.deviceId, 'dev1');
    expect(store.saved!.token, 'secret-token');
    expect(store.saved!.macName, 'Studio');
    expect(store.saved!.fingerprint, 'a' * 64);
    expect(mac.seen.single.bodyText, contains('"stepUpPublicKey":"cHVibGljLWtleQ=="'));
    expect(mac.seen.single.bodyText, contains('"deviceName":"Test phone"'));
    expect(store.saved.toString(), isNot(contains('secret-token')));
  });

  test('a refused pairing leaves nothing behind and removes the key', () async {
    mac.answers['POST /remote/v1/pair'] = (403, {'error': 'pairing-closed'});
    await expectLater(
      session.pair(payload()),
      throwsA(isA<RemoteFailure>().having((f) => f.kind, 'kind', FailureKind.pairingRefused)),
    );
    expect(store.saved, isNull);
    expect(signer.keyDeleted, isTrue);
    expect(session.phase, isNot(SessionPhase.paired));
  });

  test('unpairing tells the Mac, then forgets the token and the key', () async {
    mac.answers['POST /remote/v1/pair'] = (201, {'deviceId': 'dev1', 'token': 't'});
    await session.pair(payload());
    mac.seen.clear();
    await session.unpair();
    expect(mac.seen.single.method, 'DELETE');
    expect(mac.seen.single.path, '/remote/v1/device');
    expect(store.saved, isNull);
    expect(signer.keyDeleted, isTrue);
    expect(session.phase, SessionPhase.unpaired);
  });

  test('unpairing still works when the Mac is away', () async {
    mac.answers['POST /remote/v1/pair'] = (201, {'deviceId': 'dev1', 'token': 't'});
    await session.pair(payload());
    await mac.close();
    await session.unpair();
    expect(store.saved, isNull);
    expect(session.phase, SessionPhase.unpaired);
    mac = await FakeMac.start();
  });

  test('a revocation explains itself on the pairing screen', () async {
    mac.answers['POST /remote/v1/pair'] = (201, {'deviceId': 'dev1', 'token': 't'});
    await session.pair(payload());
    await session.revoked();
    expect(session.phase, SessionPhase.unpaired);
    expect(session.notice, contains('unpaired'));
  });

  test('device names are made acceptable to the Mac', () {
    expect(sanitizeDeviceName('Google Pixel 7'), 'Google Pixel 7');
    expect(sanitizeDeviceName('Arthur’s phone 📱'), 'Arthur’s phone');
    expect(sanitizeDeviceName('📱'), 'Android phone');
    expect(sanitizeDeviceName('x' * 100).length, 64);
  });

  group('address update', () {
    AddressPayload address({String? fp, String ext = 'new.example.org:9000'}) =>
        AddressPayload.parse('pouple-address:v1?ext=$ext&fp=${fp ?? 'a' * 64}');

    Future<void> pairWithExternal() async {
      mac.answers['POST /remote/v1/pair'] = (201, {'deviceId': 'dev1', 'token': 'secret-token'});
      await session.pair(
        PairingPayload.parse(
          'pouple-pair:v1?hosts=127.0.0.1&port=${mac.port}&fp=${'a' * 64}&code=${'b' * 32}&name=Studio'
          '&ext=old.example.org:8000',
        ),
      );
    }

    test('pairing keeps the external address of the code', () async {
      await pairWithExternal();
      expect(store.saved!.external.toString(), 'old.example.org:8000');
    });

    test('replaces only the external address, never the token, the device or the home addresses', () async {
      await pairWithExternal();
      final before = store.saved!;
      await session.applyAddress(address(fp: ('A' * 64)));
      final after = store.saved!;
      expect(after.external.toString(), 'new.example.org:9000');
      expect(session.credentials!.external, after.external);
      expect(after.token, before.token);
      expect(after.deviceId, before.deviceId);
      expect(after.hosts, before.hosts);
      expect(after.port, before.port);
      expect(after.fingerprint, before.fingerprint);
      expect(signer.keyDeleted, isFalse);
    });

    test('refuses a code from another Mac and changes nothing', () async {
      await pairWithExternal();
      await expectLater(
        session.applyAddress(address(fp: 'c' * 64)),
        throwsA(isA<AddressUpdateException>().having((e) => e.reason, 'reason', contains('another Mac'))),
      );
      expect(store.saved!.external.toString(), 'old.example.org:8000');
    });

    test('is refused politely when the phone is not paired', () async {
      await session.restore();
      await expectLater(session.applyAddress(address()), throwsA(isA<AddressUpdateException>()));
      expect(store.saved, isNull);
    });

    test('an address learned from the Mac is stored the same way', () async {
      await pairWithExternal();
      await session.updateExternal(ExternalAddress.parse('learned.example.org:7000'));
      expect(store.saved!.external.toString(), 'learned.example.org:7000');
      expect(store.saved!.token, 'secret-token');
    });
  });

  group('tunnel domain', () {
    Future<void> pairWithTunnel({String extra = ''}) async {
      mac.answers['POST /remote/v1/pair'] = (201, {'deviceId': 'dev1', 'token': 'secret-token'});
      await session.pair(
        PairingPayload.parse(
          'pouple-pair:v1?hosts=127.0.0.1&port=${mac.port}&fp=${'a' * 64}&code=${'b' * 32}&name=Studio'
          '&ext=old.example.org:8000$extra',
        ),
      );
    }

    test('pairing keeps the tunnel domain of the code', () async {
      await pairWithTunnel(extra: '&tun=old.ngrok-free.app');
      expect(store.saved!.tunnel, 'old.ngrok-free.app');
    });

    test('an address code with a tunnel replaces it, and keeps the external address', () async {
      await pairWithTunnel(extra: '&tun=old.ngrok-free.app');
      await session.applyAddress(
        AddressPayload.parse(
          'pouple-address:v1?ext=new.example.org:9000&fp=${'a' * 64}&tun=new.ngrok-free.app',
        ),
      );
      expect(store.saved!.tunnel, 'new.ngrok-free.app');
      expect(store.saved!.external.toString(), 'new.example.org:9000');
      expect(store.saved!.token, 'secret-token');
    });

    test('an address code without a tunnel leaves the stored one alone', () async {
      await pairWithTunnel(extra: '&tun=old.ngrok-free.app');
      await session.applyAddress(
        AddressPayload.parse('pouple-address:v1?ext=new.example.org:9000&fp=${'a' * 64}'),
      );
      expect(store.saved!.tunnel, 'old.ngrok-free.app');
    });

    test('a tunnel domain learned from the Mac is stored the same way', () async {
      await pairWithTunnel();
      expect(store.saved!.tunnel, isNull);
      await session.updateTunnel('learned.ngrok-free.app');
      expect(store.saved!.tunnel, 'learned.ngrok-free.app');
      expect(store.saved!.external.toString(), 'old.example.org:8000');
    });
  });
}
