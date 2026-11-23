import 'dart:async';

import 'package:flutter/foundation.dart';

import '../data/models/assistant.dart';
import '../data/net/remote_failure.dart';
import 'backoff.dart';
import 'decisions.dart';

/// Whether the live link to the Mac works.
enum LinkState { idle, connecting, live, unreachable }

/// The conversations as the Mac streams them, while the app is in the foreground.
///
/// Reconnects with a growing delay and remembers since when the Mac has been unreachable.
class ConversationsController extends ChangeNotifier {
  ConversationsController({
    required this._openStream,
    required this._fetch,
    required this._onRevoked,
    Backoff? backoff,
    Future<void> Function(Duration)? delay,
    DateTime Function()? now,
  }) : _backoff = backoff ?? Backoff(),
       _delay = delay ?? Future<void>.delayed,
       _now = now ?? DateTime.now;

  final Stream<AssistantSnapshot> Function() _openStream;
  final Future<AssistantSnapshot> Function() _fetch;
  final Future<void> Function() _onRevoked;
  final Backoff _backoff;
  final Future<void> Function(Duration) _delay;
  final DateTime Function() _now;

  AssistantSnapshot? _snapshot;
  LinkState _link = LinkState.idle;
  DateTime? _unreachableSince;
  RemoteFailure? _failure;
  int _generation = 0;
  StreamSubscription<AssistantSnapshot>? _subscription;
  Completer<void>? _pending;
  bool _disposed = false;

  AssistantSnapshot? get snapshot => _snapshot;
  LinkState get link => _link;

  /// Since when no snapshot could be had; null while the link works.
  DateTime? get unreachableSince => _unreachableSince;

  /// Why the last attempt failed, to tell a sleeping Mac from a changed certificate.
  RemoteFailure? get failure => _failure;

  /// The conversations, newest activity first.
  List<AssistantRequest> get conversations => byRecentActivity(_snapshot?.requests ?? const []);

  int get needsYou => needsYouCount(_snapshot?.requests ?? const []);

  AssistantRequest? byId(String id) => _snapshot?.byId(id);

  /// Starts following the Mac. Does nothing when it already does.
  void start() {
    if (_link != LinkState.idle || _disposed) return;
    _link = LinkState.connecting;
    _backoff.reset();
    notifyListeners();
    unawaited(_run(++_generation));
  }

  /// Stops following (the app left the foreground). Keeps the last snapshot.
  void stop() {
    _generation++;
    _release();
    if (_link != LinkState.idle) {
      _link = LinkState.idle;
      if (!_disposed) notifyListeners();
    }
  }

  /// Reads the conversations once, for after an action, without waiting for the stream.
  Future<void> refresh() async {
    try {
      final snapshot = await _fetch();
      _accept(snapshot);
    } on RemoteFailure catch (failure) {
      if (failure.kind == FailureKind.unauthorized) await _onRevoked();
    }
  }

  void _accept(AssistantSnapshot snapshot) {
    if (_disposed) return;
    _snapshot = snapshot;
    notifyListeners();
  }

  Future<void> _run(int generation) async {
    while (generation == _generation && !_disposed) {
      try {
        await _follow(generation);
      } on RemoteFailure catch (failure) {
        if (generation != _generation) return;
        if (failure.kind == FailureKind.unauthorized) {
          stop();
          await _onRevoked();
          return;
        }
        _markDown(failure);
      } on Object {
        if (generation != _generation) return;
        _markDown(const RemoteFailure(FailureKind.server));
      }
      if (generation != _generation) return;
      // The stream ended without an error: the link dropped.
      if (_link == LinkState.live) _markDown(const RemoteFailure(FailureKind.unreachable));
      await _delay(_backoff.next());
    }
  }

  /// Cancels the open stream and lets the loop waiting on it finish.
  void _release() {
    unawaited(_subscription?.cancel());
    _subscription = null;
    final pending = _pending;
    _pending = null;
    if (pending != null && !pending.isCompleted) pending.complete();
  }

  Future<void> _follow(int generation) {
    final done = Completer<void>();
    _pending = done;
    _subscription = _openStream().listen(
      (snapshot) {
        if (generation != _generation) return;
        _backoff.reset();
        _link = LinkState.live;
        _unreachableSince = null;
        _failure = null;
        _accept(snapshot);
      },
      onError: (Object error, StackTrace stack) {
        if (!done.isCompleted) done.completeError(error, stack);
      },
      onDone: () {
        if (!done.isCompleted) done.complete();
      },
      cancelOnError: true,
    );
    return done.future;
  }

  void _markDown(RemoteFailure failure) {
    if (_disposed) return;
    _failure = failure;
    _unreachableSince ??= _now();
    _link = LinkState.unreachable;
    notifyListeners();
  }

  @override
  void dispose() {
    _disposed = true;
    _generation++;
    _release();
    super.dispose();
  }
}
