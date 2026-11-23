import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../data/models/assistant.dart';
import '../data/net/remote_client.dart';
import '../domain/conversations_controller.dart';
import '../domain/decisions.dart';
import '../domain/session.dart';
import 'conversation_screen.dart';
import 'new_conversation_screen.dart';
import 'settings_screen.dart';
import 'scoped_route.dart';
import 'status_banner.dart';
import 'status_chip.dart';
import 'theme.dart';

class ConversationListScreen extends StatelessWidget {
  const ConversationListScreen({super.key});

  @override
  Widget build(BuildContext context) {
    final controller = context.watch<ConversationsController>();
    final conversations = controller.conversations;
    return Scaffold(
      body: SafeArea(
        child: Column(
          children: [
            const _Header(),
            LinkBanner(
              controller: controller,
              onRepair: () => _confirmRepair(context),
              onUpdateAddress: () => openAddressUpdate(context),
            ),
            Expanded(
              child: _Body(controller: controller, conversations: conversations),
            ),
            Padding(
              padding: const EdgeInsets.fromLTRB(20, 12, 20, 20),
              child: SizedBox(
                width: double.infinity,
                child: FilledButton.icon(
                  key: const Key('new-conversation'),
                  onPressed: () =>
                      Navigator.of(context)
                          .push(scopedRoute<void>(context, (_) => const NewConversationScreen())),
                  icon: const Icon(Icons.add, size: 18),
                  label: const Text('New conversation'),
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// "Pair again" deletes the token and the key of this phone: ask first.
Future<void> _confirmRepair(BuildContext context) async {
  final session = context.read<SessionController>();
  final confirmed = await showDialog<bool>(
    context: context,
    builder: (context) => AlertDialog(
      title: const Text('Pair again?'),
      content: const Text(
        'This phone will forget this Mac: its token and fingerprint key are deleted, and you will have to pair '
        'it again with a new code. Do this only if you know the Mac was reinstalled or reset.',
      ),
      actions: [
        TextButton(onPressed: () => Navigator.pop(context, false), child: const Text('Cancel')),
        FilledButton(onPressed: () => Navigator.pop(context, true), child: const Text('Forget this Mac')),
      ],
    ),
  );
  if (confirmed == true) await session.unpair();
}

/// The wordmark and the connection chip: the Mac's name and whether it is reached at home or away.
class _Header extends StatelessWidget {
  const _Header();

  @override
  Widget build(BuildContext context) {
    final session = context.watch<SessionController>();
    final client = Provider.of<RemoteClient?>(context, listen: false);
    final name = session.credentials?.macName ?? 'Pouple';
    final scheme = Theme.of(context).colorScheme;
    Widget chip(NetworkRoute? route) {
      final away = route == NetworkRoute.outside || route == NetworkRoute.tunnel;
      final label = route == null ? name : '$name · ${away ? 'away' : 'home'}';
      return Semantics(
        button: true,
        label: 'Connection details, $label',
        excludeSemantics: true,
        child: InkWell(
          key: const Key('connection-chip'),
          borderRadius: BorderRadius.circular(999),
          onTap: () => Navigator.of(context).push(scopedRoute<void>(context, (_) => const SettingsScreen())),
          child: ConstrainedBox(
            constraints: const BoxConstraints(minHeight: 44),
            child: Center(
              child: Container(
                padding: const EdgeInsets.fromLTRB(10, 6, 12, 6),
                decoration: BoxDecoration(
                  color: scheme.surfaceContainerLow,
                  border: Border.all(color: scheme.outlineVariant),
                  borderRadius: BorderRadius.circular(999),
                ),
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Container(
                      width: 7,
                      height: 7,
                      decoration: BoxDecoration(
                        shape: BoxShape.circle,
                        color: route == null ? scheme.outline : poupleGreen,
                      ),
                    ),
                    const SizedBox(width: 8),
                    Flexible(
                      child: Text(
                        label,
                        overflow: TextOverflow.ellipsis,
                        style: const TextStyle(fontSize: 12),
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ),
        ),
      );
    }

    return Padding(
      padding: const EdgeInsets.fromLTRB(20, 12, 12, 4),
      child: Row(
        mainAxisAlignment: MainAxisAlignment.spaceBetween,
        children: [
          Text(
            'Pouple',
            style: TextStyle(fontWeight: FontWeight.w600, fontSize: 20, color: scheme.onSurface),
          ),
          const SizedBox(width: 12),
          Flexible(
            child: client == null
                ? chip(null)
                : ValueListenableBuilder<NetworkRoute?>(
                    valueListenable: client.route,
                    builder: (context, route, _) => chip(route),
                  ),
          ),
        ],
      ),
    );
  }
}

class _Body extends StatelessWidget {
  const _Body({required this.controller, required this.conversations});

  final ConversationsController controller;
  final List<AssistantRequest> conversations;

  @override
  Widget build(BuildContext context) {
    if (controller.snapshot == null) {
      if (controller.link == LinkState.unreachable) {
        return const Center(child: Text('Waiting for the Mac…'));
      }
      return const Center(child: CircularProgressIndicator());
    }
    if (conversations.isEmpty) {
      return const Center(child: Text('No conversations yet.'));
    }
    final needs = conversations.where(needsYou).toList();
    final others = conversations.where((request) => !needsYou(request)).toList();
    return RefreshIndicator(
      onRefresh: controller.refresh,
      child: ListView(
        padding: const EdgeInsets.fromLTRB(20, 8, 20, 16),
        children: [
          if (needs.isNotEmpty) ...[
            Row(
              children: [
                Text('NEEDS YOU', style: sectionLabelStyle(context)),
                const SizedBox(width: 8),
                Container(
                  key: const Key('needs-you-badge'),
                  padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 1),
                  decoration: BoxDecoration(
                    color: Theme.of(context).colorScheme.primary,
                    borderRadius: BorderRadius.circular(999),
                  ),
                  child: Text(
                    '${needs.length}',
                    style: TextStyle(
                      fontSize: 11,
                      fontWeight: FontWeight.w600,
                      color: Theme.of(context).colorScheme.onPrimary,
                    ),
                  ),
                ),
              ],
            ),
            const SizedBox(height: 8),
            for (final request in needs) ...[NeedsYouCard(request: request), const SizedBox(height: 8)],
            const SizedBox(height: 12),
          ],
          if (others.isNotEmpty) ...[
            Text('OTHER CONVERSATIONS', style: sectionLabelStyle(context)),
            const SizedBox(height: 4),
            for (final request in others) ConversationTile(request: request),
          ],
        ],
      ),
    );
  }
}

void _open(BuildContext context, AssistantRequest request) =>
    Navigator.of(context).push(scopedRoute<void>(context, (_) => ConversationScreen(requestId: request.id)));

/// "4 min ago" for an ISO timestamp, or null when it cannot be read.
String? agoText(String iso, {DateTime? now}) {
  final at = DateTime.tryParse(iso);
  if (at == null) return null;
  final minutes = (now ?? DateTime.now()).difference(at).inMinutes;
  if (minutes < 1) return 'just now';
  if (minutes < 60) return '$minutes min ago';
  if (minutes < 60 * 24) return '${minutes ~/ 60} h ago';
  return '${minutes ~/ (60 * 24)} d ago';
}

/// Where and from whom a conversation comes: "#front · Nadia · 4 min ago".
String metaLine(AssistantRequest request) => [
  request.source.channelName,
  request.source.senderName,
  agoText(request.updatedAt),
].whereType<String>().where((part) => part.isNotEmpty).join(' · ');

/// What a waiting conversation asks of the person, in one pill.
String needsPill(AssistantRequest request) {
  if (request.status == AssistantStatus.failed) return 'Failed';
  return switch (pendingDecision(request)) {
    Decision.plan => 'Approve plan',
    Decision.proposal => 'Approve change',
    Decision.questions => 'Question',
    null => statusLabel(request.status),
  };
}

/// A conversation that waits for the person.
class NeedsYouCard extends StatelessWidget {
  const NeedsYouCard({super.key, required this.request});

  final AssistantRequest request;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final failed = request.status == AssistantStatus.failed;
    final accent = failed ? scheme.error : scheme.primary;
    final summary = request.summary;
    final meta = metaLine(request);
    return Material(
      key: ValueKey('conversation-${request.id}'),
      color: scheme.surfaceContainerLow,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(16),
        side: BorderSide(color: scheme.outlineVariant),
      ),
      child: InkWell(
        borderRadius: BorderRadius.circular(16),
        onTap: () => _open(context, request),
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 14),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Expanded(
                    child: Text(
                      request.title,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(fontSize: 14.5, fontWeight: FontWeight.w600),
                    ),
                  ),
                  const SizedBox(width: 10),
                  Container(
                    padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 3),
                    decoration: BoxDecoration(
                      color: accent.withValues(alpha: 0.16),
                      borderRadius: BorderRadius.circular(999),
                    ),
                    child: Text(
                      needsPill(request),
                      style: TextStyle(fontSize: 11.5, fontWeight: FontWeight.w600, color: accent),
                    ),
                  ),
                ],
              ),
              if (summary != null && summary.isNotEmpty) ...[
                const SizedBox(height: 6),
                Text(
                  summary,
                  maxLines: 2,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(fontSize: 12.5, height: 1.45, color: scheme.onSurfaceVariant),
                ),
              ],
              if (meta.isNotEmpty) ...[
                const SizedBox(height: 6),
                Text(meta, style: TextStyle(fontSize: 11.5, color: scheme.outline)),
              ],
            ],
          ),
        ),
      ),
    );
  }
}

/// A conversation that does not wait for the person: a status dot, the title and where it stands.
class ConversationTile extends StatelessWidget {
  const ConversationTile({super.key, required this.request});

  final AssistantRequest request;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final working = request.isWorking;
    final ready = request.status == AssistantStatus.ready;
    final label = ready ? 'Answer ready' : statusLabel(request.status);
    final summary = request.summary;
    final detail = summary == null || summary.isEmpty ? label : '$label · $summary';
    return InkWell(
      key: ValueKey('conversation-${request.id}'),
      onTap: () => _open(context, request),
      child: ConstrainedBox(
        constraints: const BoxConstraints(minHeight: 56),
        child: Container(
          padding: const EdgeInsets.symmetric(vertical: 14),
          decoration: BoxDecoration(
            border: Border(bottom: BorderSide(color: scheme.outlineVariant.withValues(alpha: 0.6))),
          ),
          child: Row(
            children: [
              Container(
                width: 8,
                height: 8,
                decoration: BoxDecoration(
                  shape: BoxShape.circle,
                  color: statusDotColor(working: working, ready: ready),
                ),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      request.title,
                      maxLines: 2,
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(fontSize: 14, fontWeight: FontWeight.w500),
                    ),
                    const SizedBox(height: 2),
                    Text(
                      detail,
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(fontSize: 12, color: scheme.onSurfaceVariant),
                    ),
                  ],
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
