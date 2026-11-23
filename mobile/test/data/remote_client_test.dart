import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:pouple_companion/src/data/models/assistant.dart';
import 'package:pouple_companion/src/data/net/assistant_api.dart';
import 'package:pouple_companion/src/data/net/remote_client.dart';
import 'package:pouple_companion/src/data/net/remote_failure.dart';
import 'package:pouple_companion/src/data/stepup/canonical_json.dart';
import 'package:pouple_companion/src/data/stepup/step_up_signer.dart';

import '../support/fake_mac.dart';
import '../support/fixtures.dart';

void main() {
  late FakeMac mac;
  late FakeSigner signer;

  setUp(() async {
    mac = await FakeMac.start();
    signer = FakeSigner();
  });
  tearDown(() => mac.close());

  Uint8List bytes(Object json) => Uint8List.fromList(utf8.encode(jsonEncode(json)));

  group('failure mapping', () {
    RemoteFailure failure(int status, Object? body) =>
        RemoteClient.failureOf(status, body == null ? Uint8List(0) : bytes(body));

    test('remote layer codes', () {
      expect(failure(401, {'error': 'unauthorized'}).kind, FailureKind.unauthorized);
      expect(failure(403, {'error': 'step-up-required'}).kind, FailureKind.stepUpRequired);
      expect(failure(403, {'error': 'step-up-refused'}).kind, FailureKind.stepUpRefused);
      expect(failure(403, {'error': 'pairing-closed'}).kind, FailureKind.pairingRefused);
      expect(failure(403, {'error': 'pairing-refused'}).kind, FailureKind.pairingRefused);
      expect(failure(409, {'error': 'too-many-devices'}).kind, FailureKind.rejected);
      expect(failure(403, {'error': 'forbidden-route'}).kind, FailureKind.rejected);
    });

    test('assistant route errors keep their own message', () {
      final conflict = failure(409, {'error': 'The proposal changed.'});
      expect(conflict.kind, FailureKind.conflict);
      expect(conflict.display, 'The proposal changed.');
      expect(failure(404, {'error': 'Not found.'}).kind, FailureKind.notFound);
      expect(failure(500, null).kind, FailureKind.server);
      expect(failure(400, {'error': 'bad text'}).kind, FailureKind.rejected);
    });

    test('an unreadable error body still maps by status', () {
      expect(RemoteClient.failureOf(401, Uint8List.fromList([0xff, 0x00])).kind, FailureKind.unauthorized);
    });
  });

  group('requests', () {
    test('sends the bearer token, but not on pairing', () async {
      mac.answers['POST /remote/v1/pair'] = (201, {'deviceId': 'dev1', 'token': 'newtoken'});
      mac.answers['GET /api/assistant'] = (200, snapshotJson([requestJson()]));
      final client = mac.client();
      addTearDown(client.close);
      final paired = await client.pair(code: 'c', deviceName: 'Pixel', stepUpPublicKey: 'k');
      expect(paired.deviceId, 'dev1');
      expect(paired.token, 'newtoken');
      expect(mac.seen.last.headers.value('authorization'), isNull);
      expect(jsonDecode(mac.seen.last.bodyText), {
        'code': 'c',
        'deviceName': 'Pixel',
        'stepUpPublicKey': 'k',
      });

      final snapshot = await client.snapshot();
      expect(snapshot.requests, hasLength(1));
      expect(mac.seen.last.headers.value('authorization'), 'Bearer tok');
      expect(mac.seen.last.headers.value('origin'), isNull);
    });

    test('maps a 401 to unauthorized', () async {
      mac.answers['GET /api/assistant'] = (401, {'error': 'unauthorized'});
      final client = mac.client();
      addTearDown(client.close);
      await expectLater(
        client.snapshot(),
        throwsA(isA<RemoteFailure>().having((f) => f.kind, 'kind', FailureKind.unauthorized)),
      );
    });

    test('an unreadable snapshot is a server failure', () async {
      mac.answers['GET /api/assistant'] = (200, {'requests': 'nope'});
      final client = mac.client();
      addTearDown(client.close);
      await expectLater(
        client.snapshot(),
        throwsA(isA<RemoteFailure>().having((f) => f.kind, 'kind', FailureKind.server)),
      );
    });

    test('falls through to the next address and remembers the one that worked', () async {
      mac.answers['GET /remote/v1/device'] = (200, {'id': 'x', 'name': 'y'});
      final client = RemoteClient(
        endpoint: Endpoint(
          hosts: ['no-such-host.invalid', '127.0.0.1'],
          port: mac.port,
          fingerprint: 'unused',
        ),
        token: 'tok',
        scheme: 'http',
      );
      addTearDown(client.close);
      await client.checkDevice();
      await client.checkDevice();
      expect(mac.seen, hasLength(2));
    });

    test('nothing answering is unreachable', () async {
      final port = mac.port;
      await mac.close();
      final client = RemoteClient(
        endpoint: Endpoint(hosts: const ['127.0.0.1'], port: port, fingerprint: 'unused'),
        token: 'tok',
        scheme: 'http',
      );
      addTearDown(client.close);
      await expectLater(
        client.checkDevice(),
        throwsA(isA<RemoteFailure>().having((f) => f.kind, 'kind', FailureKind.unreachable)),
      );
      mac = await FakeMac.start();
    });
  });

  group('step-up', () {
    AssistantApi api() => AssistantApi(client: mac.client(), signer: signer, deviceId: 'dev1');

    test('approving a plan asks for a nonce, signs the canonical JSON of the exact body and sends both', () async {
      final api = AssistantApi(client: mac.client(), signer: signer, deviceId: 'dev1');
      await api.decidePlan('a1b2c3d4e5f60718', planId: 'plan0001', approve: true, actions: [0, 2]);

      expect(mac.seen.map((s) => '${s.method} ${s.path}'), [
        'POST /remote/v1/challenge',
        'POST /api/assistant/requests/a1b2c3d4e5f60718/plan',
      ]);
      final sent = mac.seen.last;
      expect(sent.bodyText, '{"decision":"approve","planId":"plan0001","actions":[0,2]}');
      expect(sent.headers.value('X-Pouple-Nonce'), 'nonce0001');
      expect(sent.headers.value('X-Pouple-Signature'), 'c2lnbmF0dXJl');
      expect(
        signer.signed.single,
        '{"v":1,"nonce":"nonce0001","deviceId":"dev1",'
        '"route":"POST /api/assistant/requests/{id}/plan","requestId":"a1b2c3d4e5f60718","planId":"plan0001",'
        '"proposalId":"","actions":[0,2],"bodySha256":"${bodySha256(sent.body)}"}',
      );
    });

    test('a plan approved as a whole sends no actions and signs null', () async {
      await api().decidePlan('r1', planId: 'plan0001', approve: true);
      expect(mac.seen.last.bodyText, '{"decision":"approve","planId":"plan0001"}');
      expect(signer.signed.single, contains('"actions":null,'));
    });

    test('declining a plan needs no fingerprint', () async {
      await api().decidePlan('r1', planId: 'plan0001', approve: false);
      expect(signer.signed, isEmpty);
      expect(mac.seen.single.path, '/api/assistant/requests/r1/plan');
      expect(mac.seen.single.headers.value('X-Pouple-Nonce'), isNull);
    });

    test('approving a proposal signs the proposal id', () async {
      await api().decideProposal('r1', proposalId: 'prop0001', approve: true);
      expect(signer.signed.single, contains('"route":"POST /api/assistant/requests/{id}/proposal"'));
      expect(signer.signed.single, contains('"planId":"","proposalId":"prop0001","actions":null'));
    });

    test('a message to a conversation with grants asks for a step-up first', () async {
      await api().sendMessage('r1', 'Please go on', hasGrants: true);
      expect(signer.signed.single, contains('"route":"POST /api/assistant/requests/{id}/messages"'));
      expect(mac.seen.last.bodyText, '{"text":"Please go on","attachmentIds":[]}');
      expect(mac.seen.last.headers.value('X-Pouple-Nonce'), 'nonce0001');
    });

    test('a message without grants goes plain, and retries with a step-up when the Mac asks', () async {
      mac.stepUpRoutes.add('POST /api/assistant/requests/r1/messages');
      await api().sendMessage('r1', 'hello', hasGrants: false);
      expect(mac.seen.map((s) => s.path), [
        '/api/assistant/requests/r1/messages',
        '/remote/v1/challenge',
        '/api/assistant/requests/r1/messages',
      ]);
      expect(signer.signed, hasLength(1));
    });

    test('a message without grants and without demand never touches the Keystore', () async {
      await api().sendMessage('r1', 'hello', hasGrants: false);
      expect(signer.signed, isEmpty);
      expect(mac.seen, hasLength(1));
    });

    test('deleting signs an empty body and the DELETE route', () async {
      await api().remove('r1');
      expect(mac.seen.last.method, 'DELETE');
      expect(mac.seen.last.body, isEmpty);
      expect(signer.signed.single, contains('"route":"DELETE /api/assistant/requests/{id}"'));
      expect(
        signer.signed.single,
        contains('"bodySha256":"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"'),
      );
    });

    test('a new conversation uses an empty requestId and returns the id', () async {
      mac.answers['POST /api/assistant/requests'] = (201, {'id': 'newid0001'});
      final id = await api().createConversation('Fix it', projectId: 'web', needsStepUp: true);
      expect(id, 'newid0001');
      expect(signer.signed.single, contains('"route":"POST /api/assistant/requests","requestId":""'));
      expect(mac.seen.last.bodyText, '{"text":"Fix it","attachmentIds":[],"projectId":"web"}');
    });

    test('a cancelled fingerprint prompt sends nothing', () async {
      signer.failure = const SignerException(SignerProblem.cancelled);
      await expectLater(
        api().decidePlan('r1', planId: 'plan0001', approve: true),
        throwsA(isA<SignerException>()),
      );
      expect(mac.seen.map((s) => s.path), ['/remote/v1/challenge']);
    });

    test('an id that would need escaping is refused before any prompt', () async {
      await expectLater(
        api().decideProposal('r1', proposalId: 'bad"id', approve: true),
        throwsA(isA<StepUpFormatException>()),
      );
      expect(signer.signed, isEmpty);
    });

    test('answers carry single, multiple and free answers', () async {
      await api().answer('r1', [
        const QuestionAnswerInput(questionId: 'q1', optionId: 'a'),
        const QuestionAnswerInput(questionId: 'q2', optionIds: ['x', 'y']),
        const QuestionAnswerInput(questionId: 'q3', text: 'my own words'),
      ], hasGrants: false);
      expect(jsonDecode(mac.seen.single.bodyText), {
        'answers': [
          {'questionId': 'q1', 'optionId': 'a'},
          {
            'questionId': 'q2',
            'optionIds': ['x', 'y'],
          },
          {'questionId': 'q3', 'text': 'my own words'},
        ],
      });
    });
  });

  test('the conversation id is URL-encoded in the path', () async {
    await AssistantApi(client: mac.client(), signer: signer, deviceId: 'd').stop('a/b c');
    expect(mac.seen.single.path, '/api/assistant/requests/a%2Fb%20c/stop');
  });

  test('projects parse', () async {
    mac.answers['GET /api/assistant/projects'] = (
      200,
      {
        'projects': [
          {'id': 'web', 'name': 'web'},
        ],
      },
    );
    final client = mac.client();
    addTearDown(client.close);
    final projects = await client.projects();
    expect(projects.single, isA<ProjectInfo>().having((p) => p.id, 'id', 'web'));
  });
}
