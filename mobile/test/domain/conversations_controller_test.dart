import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:pouple_companion/src/data/models/assistant.dart';
import 'package:pouple_companion/src/data/net/remote_failure.dart';
import 'package:pouple_companion/src/domain/backoff.dart';
import 'package:pouple_companion/src/domain/conversations_controller.dart';

import '../support/fixtures.dart';

AssistantSnapshot snap(String status) =>
    AssistantSnapshot.fromJson(snapshotJson([requestJson(status: status)]));

void main() {
  group('Backoff', () {
    test('doubles up to the ceiling and resets', () {
      final backoff = Backoff();
      expect([for (var i = 0; i < 8; i++) backoff.next().inSeconds], [1, 2, 4, 8, 16, 30, 30, 30]);
      backoff.reset();
      expect(backoff.next().inSeconds, 1);
    });
  });

  group('ConversationsController', () {
    late List<Duration> delays;
    late List<StreamController<AssistantSnapshot>> opened;
    late int revoked;
    late DateTime clock;
    late Object? Function(int attempt) failOpen;

    ConversationsController build() => ConversationsController(
      openStream: () {
        final attempt = opened.length;
        final controller = StreamController<AssistantSnapshot>();
        opened.add(controller);
        final failure = failOpen(attempt);
        if (failure != null) scheduleMicrotask(() => controller.addError(failure));
        return controller.stream;
      },
      fetch: () async => snap('ready'),
      onRevoked: () async => revoked++,
      delay: (duration) async {
        delays.add(duration);
        // Yield to the event loop, as a real wait would.
        await Future<void>.delayed(Duration.zero);
      },
      now: () => clock,
    );

    setUp(() {
      delays = [];
      opened = [];
      revoked = 0;
      clock = DateTime.utc(2026, 11, 20, 10);
      failOpen = (_) => null;
    });

    Future<void> settle() => Future<void>.delayed(const Duration(milliseconds: 5));

    test('goes live on the first snapshot', () async {
      final controller = build()..start();
      expect(controller.link, LinkState.connecting);
      opened.single.add(snap('awaiting-user'));
      await settle();
      expect(controller.link, LinkState.live);
      expect(controller.needsYou, 1);
      expect(controller.conversations.single.id, 'a1b2c3d4e5f60718');
      controller.dispose();
    });

    test('a dropped link is unreachable since the first failure, then retried with backoff', () async {
      final controller = build()..start();
      opened.first.add(snap('ready'));
      await settle();
      clock = DateTime.utc(2026, 11, 20, 10, 5);
      await opened.first.close();
      await settle();
      expect(controller.link, LinkState.unreachable);
      expect(controller.unreachableSince, DateTime.utc(2026, 11, 20, 10, 5));
      expect(delays, [const Duration(seconds: 1)]);
      expect(opened, hasLength(2));

      // The Mac stays away: the same "since" and a longer wait.
      clock = DateTime.utc(2026, 11, 20, 10, 9);
      opened[1].addError(const RemoteFailure(FailureKind.unreachable));
      await settle();
      expect(controller.unreachableSince, DateTime.utc(2026, 11, 20, 10, 5));
      expect(delays, [const Duration(seconds: 1), const Duration(seconds: 2)]);

      // It comes back: live again, and the next failure starts over from one second.
      opened[2].add(snap('ready'));
      await settle();
      expect(controller.link, LinkState.live);
      expect(controller.unreachableSince, isNull);
      await opened[2].close();
      await settle();
      expect(delays.last, const Duration(seconds: 1));
      controller.dispose();
    });

    test('keeps the last snapshot while unreachable', () async {
      final controller = build()..start();
      opened.first.add(snap('done'));
      await settle();
      opened.first.addError(const RemoteFailure(FailureKind.unreachable));
      await settle();
      expect(controller.snapshot, isNotNull);
      expect(controller.link, LinkState.unreachable);
      controller.dispose();
    });

    test('an unauthorized stream stops retrying and reports the revocation', () async {
      failOpen = (_) => const RemoteFailure(FailureKind.unauthorized);
      final controller = build()..start();
      await settle();
      await settle();
      expect(revoked, 1);
      expect(opened, hasLength(1));
      expect(controller.link, LinkState.idle);
      controller.dispose();
    });

    test('a certificate mismatch is reported as the failure', () async {
      failOpen = (_) => const RemoteFailure(FailureKind.certificateMismatch);
      final controller = build()..start();
      await settle();
      expect(controller.failure?.kind, FailureKind.certificateMismatch);
      controller.dispose();
    });

    test('stop cancels the stream and start follows again', () async {
      final controller = build()..start();
      opened.first.add(snap('ready'));
      await settle();
      controller.stop();
      expect(controller.link, LinkState.idle);
      expect(opened.first.hasListener, isFalse);
      controller.start();
      expect(opened, hasLength(2));
      controller.dispose();
    });

    test('start twice does not open two streams', () async {
      final controller = build()
        ..start()
        ..start();
      expect(opened, hasLength(1));
      controller.dispose();
    });

    test('an unexpected error is a failure to retry, not a crash', () async {
      failOpen = (attempt) => attempt == 0 ? const FormatException('bad') : null;
      final controller = build()..start();
      await settle();
      expect(controller.link, LinkState.unreachable);
      expect(opened, hasLength(2));
      controller.dispose();
    });
  });
}
