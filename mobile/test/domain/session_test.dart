import 'package:flutter_test/flutter_test.dart';
import 'package:pouple_companion/src/data/net/remote_failure.dart';
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
}
