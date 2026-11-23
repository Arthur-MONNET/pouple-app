import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../data/models/assistant.dart';
import '../domain/conversations_controller.dart';
import '../domain/decisions.dart';
import '../domain/session.dart';
import 'conversation_screen.dart';
import 'new_conversation_screen.dart';
import 'settings_screen.dart';
import 'scoped_route.dart';
import 'status_banner.dart';
import 'status_chip.dart';

class ConversationListScreen extends StatelessWidget {
  const ConversationListScreen({super.key});

  @override
  Widget build(BuildContext context) {
    final controller = context.watch<ConversationsController>();
    final session = context.watch<SessionController>();
    final conversations = controller.conversations;
    final needs = controller.needsYou;
    return Scaffold(
      appBar: AppBar(
        title: Row(
          children: [
            Flexible(child: Text(session.credentials?.macName ?? 'Pouple', overflow: TextOverflow.ellipsis)),
            if (needs > 0) ...[
              const SizedBox(width: 10),
              Badge(label: Text('$needs'), key: const Key('needs-you-badge')),
            ],
          ],
        ),
        actions: [
          IconButton(
            tooltip: 'Settings',
            icon: const Icon(Icons.settings_outlined),
            onPressed: () =>
                Navigator.of(context).push(scopedRoute<void>(context, (_) => const SettingsScreen())),
          ),
        ],
      ),
      body: Column(
        children: [
          LinkBanner(
            controller: controller,
            onRepair: () => context.read<SessionController>().unpair(),
            onUpdateAddress: () => openAddressUpdate(context),
          ),
          Expanded(
            child: _Body(controller: controller, conversations: conversations),
          ),
        ],
      ),
      floatingActionButton: FloatingActionButton.extended(
        onPressed: () =>
            Navigator.of(context).push(scopedRoute<void>(context, (_) => const NewConversationScreen())),
        icon: const Icon(Icons.add),
        label: const Text('New'),
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
    return RefreshIndicator(
      onRefresh: controller.refresh,
      child: ListView.separated(
        padding: const EdgeInsets.only(bottom: 88),
        itemCount: conversations.length,
        separatorBuilder: (_, _) => const Divider(height: 1),
        itemBuilder: (context, index) => ConversationTile(request: conversations[index]),
      ),
    );
  }
}

class ConversationTile extends StatelessWidget {
  const ConversationTile({super.key, required this.request});

  final AssistantRequest request;

  @override
  Widget build(BuildContext context) {
    final needs = needsYou(request);
    final scheme = Theme.of(context).colorScheme;
    return ListTile(
      key: ValueKey('conversation-${request.id}'),
      title: Text(
        request.title,
        maxLines: 2,
        overflow: TextOverflow.ellipsis,
        style: TextStyle(fontWeight: needs ? FontWeight.w700 : null),
      ),
      subtitle: request.summary == null || request.summary!.isEmpty
          ? null
          : Text(request.summary!, maxLines: 2, overflow: TextOverflow.ellipsis),
      leading: needs
          ? Icon(Icons.notifications_active, color: scheme.primary)
          : const Icon(Icons.chat_bubble_outline),
      trailing: StatusChip(status: request.status),
      onTap: () =>
          Navigator.of(context)
              .push(scopedRoute<void>(context, (_) => ConversationScreen(requestId: request.id))),
    );
  }
}
