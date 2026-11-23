import 'package:flutter_test/flutter_test.dart';
import 'package:pouple_companion/src/data/models/assistant.dart';
import 'package:pouple_companion/src/domain/decisions.dart';

import '../support/fixtures.dart';

void main() {
  AssistantRequest build({
    String status = 'awaiting-user',
    List<Object?>? questions,
    Object? plan,
    Object? proposal,
    String updatedAt = '2026-11-20T10:00:00.000Z',
  }) => parsedRequest(
    requestJson(status: status, questions: questions, plan: plan, proposal: proposal, updatedAt: updatedAt),
  );

  group('pendingDecision', () {
    test('nothing pending', () => expect(pendingDecision(build(status: 'ready')), isNull));

    test('questions come first', () {
      final request = build(questions: [questionJson()], plan: planJson(), proposal: proposalJson());
      expect(pendingDecision(request), Decision.questions);
    });

    test('then a plan awaiting approval', () {
      expect(pendingDecision(build(plan: planJson(), proposal: proposalJson())), Decision.plan);
    });

    test('an approved plan is not pending, a proposal then is', () {
      final request = build(
        plan: planJson(status: 'approved'),
        proposal: proposalJson(),
      );
      expect(pendingDecision(request), Decision.proposal);
    });

    test('a declined plan alone is not pending', () {
      expect(pendingDecision(build(plan: planJson(status: 'declined'))), isNull);
    });
  });

  group('plan question', () {
    test('a single action is a yes/no with the code wording', () {
      final question = planQuestion(build(plan: planJson()))!;
      expect(question.multiple, isFalse);
      expect(question.options.map((o) => o.id), ['approve', 'decline']);
      expect(question.options.first.label, 'Yes, write it, commit it and push the branch');
    });

    test('several actions are a multiple pick with one option per action', () {
      final request = build(plan: planJson(actions: 3));
      expect(planIsPicked(request), isTrue);
      final question = planQuestion(request)!;
      expect(question.multiple, isTrue);
      expect(question.options.map((o) => o.id), ['action-0', 'action-1', 'action-2']);
      expect(question.options.first.label, 'Write code · fix/login ← main');
      expect(question.options[1].label, 'Open a merge request · fix/login → main');
    });

    test('a merge request alone has its own wording', () {
      final json = planJson()
        ..['actions'] = [
          {
            'kind': 'gitlab.merge-request',
            'scope': {'service': 'gitlab', 'id': 'g/p'},
            'summary': 's',
            'branch': 'a',
            'base': 'b',
          },
        ];
      expect(planQuestion(build(plan: json))!.options.first.label, 'Yes, open the merge request');
    });

    test('picked actions are sorted positions', () {
      expect(pickedActions({'action-2', 'action-0', 'action-10'}), [0, 2, 10]);
    });
  });

  test('proposal question names the project and title', () {
    final question = proposalQuestion(build(proposal: proposalJson()))!;
    expect(question.text, 'Apply this change in web? Fix redirect');
    expect(question.options.first.description, 'Change the route guard');
  });

  group('needs you', () {
    test('awaiting-user and failed need the person', () {
      expect(needsYou(build(status: 'awaiting-user')), isTrue);
      expect(needsYou(build(status: 'failed')), isTrue);
      expect(needsYou(build(status: 'analyzing')), isFalse);
      expect(needsYou(build(status: 'ready')), isFalse);
      expect(needsYou(build(status: 'unknown-new-status')), isFalse);
    });

    test('count', () {
      expect(needsYouCount([build(), build(status: 'failed'), build(status: 'done')]), 2);
    });

    test('deciding needs awaiting-user and something pending', () {
      expect(isDeciding(build(questions: [questionJson()])), isTrue);
      expect(isDeciding(build()), isFalse);
      expect(isDeciding(build(status: 'ready', questions: [questionJson()])), isFalse);
    });

    test('the composer is hidden while working or awaiting', () {
      expect(canWrite(build(status: 'ready')), isTrue);
      expect(canWrite(build(status: 'analyzing')), isFalse);
      expect(canWrite(build(status: 'awaiting-user')), isFalse);
    });
  });

  group('attention keys', () {
    test('only conversations that need the person, keyed by update', () {
      final keys = attentionKeys([build(), build(status: 'done')]);
      expect(keys, {'a1b2c3d4e5f60718@2026-11-20T10:00:00.000Z'});
    });

    test('a conversation that needs the person again is new', () {
      final before = attentionKeys([build()]);
      final after = attentionKeys([build(updatedAt: '2026-11-20T11:00:00.000Z')]);
      expect(newAttention(before, after), hasLength(1));
      expect(newAttention(before, before), isEmpty);
      expect(newAttention(before, <String>{}), isEmpty);
    });
  });

  test('recent activity sorts newest first', () {
    final older = build(updatedAt: '2026-11-19T10:00:00.000Z');
    final newer = build(updatedAt: '2026-11-21T10:00:00.000Z');
    expect(byRecentActivity([older, newer]).first, newer);
  });
}
