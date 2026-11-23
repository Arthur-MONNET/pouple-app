import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:pouple_companion/src/data/models/assistant.dart';
import 'package:pouple_companion/src/data/net/remote_failure.dart';
import 'package:pouple_companion/src/data/storage/credentials.dart';
import 'package:pouple_companion/src/domain/app_lock.dart';
import 'package:pouple_companion/src/domain/conversation_actions.dart';
import 'package:pouple_companion/src/domain/conversations_controller.dart';
import 'package:pouple_companion/src/domain/session.dart';
import 'package:pouple_companion/src/ui/conversation_list_screen.dart';
import 'package:pouple_companion/src/ui/pairing_screen.dart';
import 'package:pouple_companion/src/ui/theme.dart';
import 'package:provider/provider.dart';

import '../support/fake_api.dart';
import '../support/fake_mac.dart';
import '../support/fixtures.dart';

class _EmptyStore implements CredentialStore {
  @override
  Future<Credentials?> load() async => null;

  @override
  Future<void> save(Credentials credentials) async {}

  @override
  Future<void> clear() async {}
}

class _Harness {
  _Harness(this.snapshot, {this.failure}) {
    controller = ConversationsController(
      openStream: () {
        if (failure != null) return Stream.error(failure!);
        return stream.stream;
      },
      fetch: () async => snapshot,
      onRevoked: () async {},
      delay: (_) => Completer<void>().future,
      now: () => DateTime.utc(2026, 11, 20, 9, 41),
    );
    actions = ConversationActions(api: api, refresh: () async {}, onRevoked: () async {});
  }

  final AssistantSnapshot snapshot;
  final RemoteFailure? failure;
  final stream = StreamController<AssistantSnapshot>();
  final api = FakeApi();
  late final ConversationsController controller;
  late final ConversationActions actions;

  Future<void> pump(WidgetTester tester) async {
    final signer = FakeSigner();
    addTearDown(() {
      controller.dispose();
      actions.dispose();
      stream.close();
    });
    await tester.pumpWidget(
      MultiProvider(
        providers: [
          ChangeNotifierProvider<SessionController>(
            create: (_) => SessionController(store: _EmptyStore(), signer: signer),
          ),
          ChangeNotifierProvider<AppLock>(create: (_) => AppLock(signer: signer)),
          ChangeNotifierProvider<ConversationsController>.value(value: controller),
          ChangeNotifierProvider<ConversationActions>.value(value: actions),
        ],
        child: MaterialApp(theme: poupleTheme(Brightness.light), home: const ConversationListScreen()),
      ),
    );
    controller.start();
    if (failure == null) stream.add(snapshot);
    await tester.pump();
    await tester.pump();
  }
}

AssistantSnapshot snapshotOf(List<Map<String, Object?>> requests) =>
    AssistantSnapshot.fromJson(snapshotJson(requests));

void main() {
  testWidgets('the list shows the needs-you badge and puts recent activity first', (tester) async {
    final harness = _Harness(
      snapshotOf([
        requestJson(id: 'old00001', status: 'done', updatedAt: '2026-11-19T10:00:00.000Z', text: 'Old thing'),
        requestJson(
            id: 'new00001',
            status: 'awaiting-user',
            updatedAt: '2026-11-21T10:00:00.000Z',
            text: 'New thing',
            questions: [questionJson()],
          )
          ..['source'] = {
            'origin': 'in-app',
            'channelId': 'x',
            'senderId': 'self',
            'senderName': '',
            'label': 'New thing',
          },
        requestJson(id: 'bad00001', status: 'failed', updatedAt: '2026-11-20T10:00:00.000Z', text: 'Broken')
          ..['source'] = {
            'origin': 'in-app',
            'channelId': 'x',
            'senderId': 'self',
            'senderName': '',
            'label': 'Broken',
          },
      ]),
    );
    await harness.pump(tester);
    expect(find.byKey(const Key('needs-you-badge')), findsOneWidget);
    expect(find.text('2'), findsOneWidget);
    expect(find.text('Your call'), findsOneWidget);
    expect(find.text('Failed'), findsOneWidget);
    final first = tester.getTopLeft(find.byKey(const ValueKey('conversation-new00001'))).dy;
    final second = tester.getTopLeft(find.byKey(const ValueKey('conversation-bad00001'))).dy;
    expect(first, lessThan(second));
  });

  testWidgets('an empty list says so, with no badge', (tester) async {
    final harness = _Harness(snapshotOf([]));
    await harness.pump(tester);
    expect(find.text('No conversations yet.'), findsOneWidget);
    expect(find.byKey(const Key('needs-you-badge')), findsNothing);
  });

  testWidgets('the banner tells since when the Mac is unreachable', (tester) async {
    final harness = _Harness(snapshotOf([]), failure: const RemoteFailure(FailureKind.unreachable));
    await harness.pump(tester);
    expect(find.textContaining('Mac unreachable since'), findsOneWidget);
  });

  testWidgets('a changed certificate is not presented as a sleeping Mac', (tester) async {
    final harness = _Harness(snapshotOf([]), failure: const RemoteFailure(FailureKind.certificateMismatch));
    await harness.pump(tester);
    expect(find.textContaining('certificate changed'), findsOneWidget);
    expect(find.text('Pair again'), findsOneWidget);
  });

  group('conversation', () {
    Future<_Harness> open(WidgetTester tester, Map<String, Object?> request) async {
      final harness = _Harness(snapshotOf([request]));
      await harness.pump(tester);
      await tester.tap(find.byKey(ValueKey('conversation-${request['id']}')));
      // Not pumpAndSettle: a working conversation shows a spinner that never settles.
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 500));
      return harness;
    }

    testWidgets('a single choice question is answered with the picked option', (tester) async {
      final harness = await open(tester, requestJson(status: 'awaiting-user', questions: [questionJson()]));
      expect(find.text('Which environment?'), findsOneWidget);
      expect(find.byKey(const Key('send-message')), findsNothing, reason: 'the card replaces the composer');
      final send = find.widgetWithText(FilledButton, 'Send answer');
      expect(tester.widget<FilledButton>(send).onPressed, isNull);
      await tester.tap(find.byKey(const ValueKey('option-b')));
      await tester.pump();
      await tester.tap(send);
      await tester.pump();
      expect(harness.api.calls.single, 'answer q1:b');
    });

    testWidgets('a free answer is enough', (tester) async {
      final harness = await open(tester, requestJson(status: 'awaiting-user', questions: [questionJson()]));
      await tester.enterText(find.byType(TextField), 'Neither, use preprod');
      await tester.pump();
      await tester.tap(find.widgetWithText(FilledButton, 'Send answer'));
      await tester.pump();
      expect(harness.api.calls.single, 'answer q1:text(Neither, use preprod)');
    });

    testWidgets('a multiple question takes several options', (tester) async {
      final harness = await open(
        tester,
        requestJson(status: 'awaiting-user', questions: [questionJson(multiple: true)]),
      );
      await tester.tap(find.byKey(const ValueKey('option-a')));
      await tester.tap(find.byKey(const ValueKey('option-b')));
      await tester.pump();
      await tester.tap(find.widgetWithText(FilledButton, 'Send answer'));
      await tester.pump();
      expect(harness.api.calls.single, 'answer q1:[a, b]');
    });

    testWidgets('a plan with several actions needs ticks, and approves only those', (tester) async {
      final harness = await open(tester, requestJson(status: 'awaiting-user', plan: planJson(actions: 2)));
      final approve = find.widgetWithText(FilledButton, 'Approve ticked actions');
      expect(tester.widget<FilledButton>(approve).onPressed, isNull);
      await tester.tap(find.byKey(const ValueKey('plan-action-1')));
      await tester.pump();
      await tester.tap(approve);
      await tester.pump();
      expect(harness.api.calls.single, 'plan a1b2c3d4e5f60718 plan0001 approve [1]');
    });

    testWidgets('a single-action plan can be declined without a fingerprint', (tester) async {
      final harness = await open(tester, requestJson(status: 'awaiting-user', plan: planJson()));
      expect(find.text('Plan awaiting your approval'), findsOneWidget);
      await tester.tap(find.text('No, write nothing'));
      await tester.pump();
      expect(harness.api.calls.single, 'plan a1b2c3d4e5f60718 plan0001 decline null');
    });

    testWidgets('a proposal is approved or declined with its id', (tester) async {
      final harness = await open(tester, requestJson(status: 'awaiting-user', proposal: proposalJson()));
      expect(find.text('Fix redirect'), findsOneWidget);
      await tester.tap(find.text('Yes, make the change'));
      await tester.pump();
      expect(harness.api.calls.single, 'proposal a1b2c3d4e5f60718 prop0001 approve');
    });

    testWidgets('a failure from the Mac is shown on the card', (tester) async {
      final harness = await open(tester, requestJson(status: 'awaiting-user', proposal: proposalJson()));
      harness.api.failure = const RemoteFailure(FailureKind.conflict, message: 'The proposal changed.');
      await tester.tap(find.text('Yes, make the change'));
      await tester.pump();
      await tester.pump();
      expect(find.text('The proposal changed.'), findsOneWidget);
    });

    testWidgets('a ready conversation shows the composer and sends a follow-up', (tester) async {
      final harness = await open(tester, requestJson(status: 'ready'));
      await tester.enterText(find.byType(TextField), 'Thanks, one more thing');
      await tester.pump();
      await tester.tap(find.byKey(const Key('send-message')));
      await tester.pump();
      expect(harness.api.calls.single, 'message a1b2c3d4e5f60718 Thanks, one more thing grants=false');
    });

    testWidgets('a conversation with grants asks for the fingerprint on send', (tester) async {
      final harness = await open(tester, requestJson(status: 'ready', grants: [<String, Object?>{}]));
      expect(find.byIcon(Icons.fingerprint), findsOneWidget);
      await tester.enterText(find.byType(TextField), 'Go on');
      await tester.pump();
      await tester.tap(find.byKey(const Key('send-message')));
      await tester.pump();
      expect(harness.api.calls.single, endsWith('grants=true'));
    });

    testWidgets('a working conversation can be stopped and has no composer', (tester) async {
      final harness = await open(tester, requestJson(status: 'analyzing'));
      expect(find.byKey(const Key('send-message')), findsNothing);
      await tester.tap(find.text('Stop'));
      await tester.pump();
      expect(harness.api.calls.single, 'stop a1b2c3d4e5f60718');
    });

    testWidgets('a failed conversation shows its error and can be retried', (tester) async {
      final harness = await open(tester, requestJson(status: 'failed', error: 'The AI ran out of credit.'));
      expect(find.text('The AI ran out of credit.'), findsOneWidget);
      await tester.tap(find.text('Investigate again'));
      await tester.pump();
      expect(harness.api.calls.single, 'retry a1b2c3d4e5f60718');
    });

    testWidgets('work steps are folded and messages are shown', (tester) async {
      await open(
        tester,
        requestJson(
          entries: [
            {'id': '1', 'at': 'x', 'role': 'user', 'kind': 'text', 'text': 'Fix the redirect'},
            {'id': '2', 'at': 'x', 'role': 'assistant', 'kind': 'step', 'text': 'Reading router.ts'},
            {'id': '3', 'at': 'x', 'role': 'assistant', 'kind': 'step', 'text': 'Editing router.ts'},
            {'id': '4', 'at': 'x', 'role': 'assistant', 'kind': 'text', 'text': 'Done, see the change.'},
          ],
        ),
      );
      expect(find.text('Fix the redirect'), findsWidgets);
      expect(find.text('2 steps'), findsOneWidget);
      expect(find.text('Done, see the change.'), findsOneWidget);
    });
  });

  group('pairing screen', () {
    Future<void> pumpPairing(WidgetTester tester) async {
      final signer = FakeSigner();
      await tester.pumpWidget(
        ChangeNotifierProvider<SessionController>(
          create: (_) => SessionController(store: _EmptyStore(), signer: signer),
          child: MaterialApp(home: const PairingScreen()),
        ),
      );
    }

    testWidgets('offers the scan and the paste fallback', (tester) async {
      await pumpPairing(tester);
      expect(find.text('Scan the QR code'), findsOneWidget);
      expect(find.text('Or paste the pairing text'), findsOneWidget);
    });

    testWidgets('a pasted text that is not a pairing code is explained', (tester) async {
      await pumpPairing(tester);
      await tester.enterText(find.byType(TextField), 'https://example.com');
      await tester.tap(find.widgetWithText(OutlinedButton, 'Pair'));
      await tester.pump();
      expect(find.text('This is not a Pouple pairing code.'), findsOneWidget);
    });
  });
}
