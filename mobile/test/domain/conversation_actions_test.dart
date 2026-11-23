import 'package:flutter_test/flutter_test.dart';
import 'package:pouple_companion/src/data/models/assistant.dart';
import 'package:pouple_companion/src/data/net/remote_failure.dart';
import 'package:pouple_companion/src/data/stepup/step_up_signer.dart';
import 'package:pouple_companion/src/domain/conversation_actions.dart';

import '../support/fake_api.dart';
import '../support/fixtures.dart';

void main() {
  late FakeApi api;
  late int refreshes;
  late int revoked;
  late ConversationActions actions;

  setUp(() {
    api = FakeApi();
    refreshes = 0;
    revoked = 0;
    actions = ConversationActions(
      api: api,
      refresh: () async => refreshes++,
      onRevoked: () async => revoked++,
    );
  });

  AssistantRequest request({
    Object? plan,
    Object? proposal,
    List<Object?>? questions,
    List<Object?>? grants,
  }) => parsedRequest(
    requestJson(
      status: 'awaiting-user',
      plan: plan,
      proposal: proposal,
      questions: questions,
      grants: grants,
    ),
  );

  test('a picked plan sends the ticked positions only', () async {
    final done = await actions.decidePlan(
      request(plan: planJson(actions: 3)),
      approve: true,
      ticked: {'action-2', 'action-0'},
    );
    expect(done, isTrue);
    expect(api.calls.single, 'plan a1b2c3d4e5f60718 plan0001 approve [0, 2]');
    expect(refreshes, 1);
  });

  test('approving a picked plan with nothing ticked sends nothing', () async {
    expect(await actions.decidePlan(request(plan: planJson(actions: 2)), approve: true), isFalse);
    expect(api.calls, isEmpty);
  });

  test('a single-action plan is approved as a whole, and declined without positions', () async {
    await actions.decidePlan(request(plan: planJson()), approve: true);
    await actions.decidePlan(request(plan: planJson(actions: 2)), approve: false);
    expect(api.calls, [
      'plan a1b2c3d4e5f60718 plan0001 approve null',
      'plan a1b2c3d4e5f60718 plan0001 decline null',
    ]);
  });

  test('a proposal decision carries the proposal id', () async {
    await actions.decideProposal(request(proposal: proposalJson()), approve: true);
    expect(api.calls.single, 'proposal a1b2c3d4e5f60718 prop0001 approve');
  });

  test('a message to a conversation with grants says so to the API', () async {
    await actions.send(request(grants: [<String, Object?>{}]), '  go on  ');
    expect(api.calls.single, 'message a1b2c3d4e5f60718 go on grants=true');
  });

  test('answers: single, multiple and free text', () async {
    final both = request(
      questions: [
        questionJson(id: 'q1'),
        questionJson(id: 'q2', multiple: true),
        questionJson(id: 'q3'),
      ],
    );
    final done = await actions.answerQuestions(
      both,
      choices: {
        'q1': {'a'},
        'q2': {'b', 'a'},
      },
      free: {'q3': ' my words '},
    );
    expect(done, isTrue);
    expect(api.calls.single, 'answer q1:a q2:[a, b] q3:text(my words)');
  });

  test('answering nothing sends nothing', () async {
    expect(
      await actions.answerQuestions(
        request(questions: [questionJson()]),
        choices: {},
        free: {},
      ),
      isFalse,
    );
    expect(api.calls, isEmpty);
  });

  group('failures', () {
    test('a failure is kept for display and is not thrown', () async {
      api.failure = const RemoteFailure(FailureKind.rejected, message: 'No.');
      expect(await actions.stop(request()), isFalse);
      expect(actions.error, 'No.');
      expect(actions.busy, isFalse);
      actions.clearError();
      expect(actions.error, isNull);
    });

    test('a conflict reads the conversation again', () async {
      api.failure = const RemoteFailure(FailureKind.conflict);
      await actions.decideProposal(request(proposal: proposalJson()), approve: true);
      expect(refreshes, 1);
    });

    test('an unauthorized answer unpairs', () async {
      api.failure = const RemoteFailure(FailureKind.unauthorized);
      await actions.retry(request());
      expect(revoked, 1);
    });

    test('a cancelled fingerprint prompt is not an error', () async {
      api.signerFailure = const SignerException(SignerProblem.cancelled);
      expect(await actions.decideProposal(request(proposal: proposalJson()), approve: true), isFalse);
      expect(actions.error, isNull);
    });

    test('a missing fingerprint is explained', () async {
      api.signerFailure = const SignerException(SignerProblem.unavailable);
      await actions.decideProposal(request(proposal: proposalJson()), approve: true);
      expect(actions.error, contains('not available'));
    });

    test('only one action runs at a time', () async {
      api.hold();
      final first = actions.stop(request());
      expect(actions.busy, isTrue);
      expect(await actions.retry(request()), isFalse);
      api.release();
      expect(await first, isTrue);
      expect(api.calls, ['stop a1b2c3d4e5f60718']);
    });
  });

  test('creating a conversation returns its id', () async {
    expect(await actions.create('  hello ', projectId: 'web'), 'new0001');
    expect(api.calls.single, 'create hello web');
  });
}
