import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../data/net/assistant_api.dart';
import '../data/net/remote_client.dart';
import '../data/stepup/step_up_signer.dart';
import '../data/storage/credentials.dart';
import '../domain/app_lock.dart';
import '../domain/conversation_actions.dart';
import '../domain/conversations_controller.dart';
import '../domain/decisions.dart';
import '../domain/session.dart';
import '../notifications/attention_notifier.dart';
import '../notifications/background_check.dart';
import 'conversation_list_screen.dart';

/// Everything that exists while the phone is paired: the pinned client, the live conversations (followed only while
/// the app is in the foreground), the actions and the periodic background check.
class PairedScope extends StatefulWidget {
  const PairedScope({super.key, required this.credentials, this.autoStart = true});

  final Credentials credentials;

  /// Tests turn the live connection and the background work off.
  final bool autoStart;

  @override
  State<PairedScope> createState() => _PairedScopeState();
}

class _PairedScopeState extends State<PairedScope> with WidgetsBindingObserver {
  late final RemoteClient _client;
  late final ConversationsController _conversations;
  late final ConversationActions _actions;
  late final AttentionNotifier _notifier;
  Set<String> _seen = {};

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
    final session = context.read<SessionController>();
    _client = createPairedClient(widget.credentials, onExternalChanged: session.updateExternal);
    _notifier = AttentionNotifier();
    _conversations = ConversationsController(
      openStream: _client.snapshots,
      fetch: _client.snapshot,
      onRevoked: session.revoked,
    )..addListener(_onSnapshot);
    _actions = ConversationActions(
      api: AssistantApi(
        client: _client,
        signer: context.read<StepUpSigner>(),
        deviceId: widget.credentials.deviceId,
      ),
      refresh: _conversations.refresh,
      onRevoked: session.revoked,
    );
    if (widget.autoStart) {
      _conversations.start();
      const BackgroundCheck().schedule();
      _notifier.requestPermission();
    }
  }

  @override
  void didUpdateWidget(PairedScope oldWidget) {
    super.didUpdateWidget(oldWidget);
    final external = widget.credentials.external;
    if (external != null && external != _client.endpoint.external) {
      // An address code was applied: use it now and reconnect.
      _client.useExternal(external);
      if (widget.autoStart) {
        _conversations
          ..stop()
          ..start();
      }
    }
  }

  /// What the person sees in the foreground is never announced later.
  void _onSnapshot() {
    final snapshot = _conversations.snapshot;
    if (snapshot == null) return;
    final keys = attentionKeys(snapshot.requests);
    if (keys.length == _seen.length && keys.containsAll(_seen)) return;
    _seen = keys;
    if (widget.autoStart) _notifier.markSeen(keys);
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    final lock = context.read<AppLock>();
    switch (state) {
      case AppLifecycleState.resumed:
        lock.returned();
        if (widget.autoStart) _conversations.start();
      case AppLifecycleState.paused:
      case AppLifecycleState.hidden:
        lock.left();
        _conversations.stop();
      case AppLifecycleState.inactive:
      case AppLifecycleState.detached:
        break;
    }
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _conversations
      ..removeListener(_onSnapshot)
      ..dispose();
    _actions.dispose();
    _client.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return MultiProvider(
      providers: [
        Provider<RemoteClient>.value(value: _client),
        ChangeNotifierProvider<ConversationsController>.value(value: _conversations),
        ChangeNotifierProvider<ConversationActions>.value(value: _actions),
      ],
      child: const ConversationListScreen(),
    );
  }
}
