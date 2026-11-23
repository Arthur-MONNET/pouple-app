import 'package:flutter_test/flutter_test.dart';
import 'package:pouple_companion/src/data/models/assistant.dart';

import '../support/fixtures.dart';

void main() {
  test('parses a snapshot with nulls where Go sends nil slices', () {
    final snapshot = AssistantSnapshot.fromJson(snapshotJson([requestJson()]));
    expect(snapshot.requests, hasLength(1));
    final request = snapshot.requests.single;
    expect(request.status, AssistantStatus.ready);
    expect(request.questions, isEmpty);
    expect(request.proposal, isNull);
    expect(request.plan, isNull);
    expect(request.hasGrants, isFalse);
    expect(request.title, 'Fix the login redirect');
    expect(snapshot.status.active, isTrue);
  });

  test('unknown enum values decode safely', () {
    final request = parsedRequest(
      requestJson(
        status: 'hibernating',
        entries: [
          {'id': 'e1', 'at': 'x', 'role': 'robot', 'kind': 'hologram', 'text': 'hi'},
        ],
      ),
    );
    expect(request.status, AssistantStatus.unknown);
    expect(request.entries.single.role, EntryRole.unknown);
    expect(request.entries.single.kind, EntryKind.unknown);
    expect(request.isWorking, isFalse);
  });

  test('an unknown plan status is not proposed', () {
    final request = parsedRequest(requestJson(plan: planJson(status: 'weird')));
    expect(request.plan!.status, PlanStatus.unknown);
  });

  test('reads questions, proposal, plan and grants', () {
    final request = parsedRequest(
      requestJson(
        status: 'awaiting-user',
        questions: [questionJson(multiple: true)],
        proposal: proposalJson(),
        plan: planJson(actions: 2),
        grants: [
          {'scope': {}, 'kinds': <Object?>[]},
        ],
      ),
    );
    expect(request.questions.single.multiple, isTrue);
    expect(request.questions.single.options.last.description, 'Careful');
    expect(request.proposal!.id, 'prop0001');
    expect(request.plan!.actions, hasLength(2));
    expect(request.plan!.actions.first.branch, 'fix/login');
    expect(request.hasGrants, isTrue);
  });

  test('a required field of the wrong type fails without leaking its value', () {
    final json = requestJson()..['id'] = 42;
    expect(
      () => AssistantSnapshot.fromJson(snapshotJson([json])),
      throwsA(
        isA<FormatException>().having(
          (e) => e.message,
          'message',
          allOf(contains(r'$.requests[0].id'), isNot(contains('42'))),
        ),
      ),
    );
  });

  test('a secret-looking value in a wrong field is not echoed', () {
    final json = requestJson(text: 'private words')..['status'] = {'secret': 'private words'};
    expect(
      () => AssistantSnapshot.fromJson(snapshotJson([json])),
      throwsA(isA<FormatException>().having((e) => e.message, 'message', isNot(contains('private')))),
    );
  });

  test('rejects a snapshot that is not an object', () {
    expect(() => AssistantSnapshot.fromJson([1, 2]), throwsFormatException);
  });

  test('titles follow the web rules', () {
    final mention = parsedRequest(
      requestJson(origin: 'mention')
        ..['source'] = {
          'origin': 'mention',
          'channelId': 'C1',
          'channelName': 'front',
          'senderId': 'U1',
          'senderName': 'Morgan',
        },
    );
    expect(mention.title, 'Morgan in #front');
    final pipeline = parsedRequest(requestJson(origin: 'pipeline', text: 'Pipeline failed\nmore'));
    expect(pipeline.source.isEvent, isTrue);
    expect(pipeline.title, 'Pipeline failed');
  });
}
