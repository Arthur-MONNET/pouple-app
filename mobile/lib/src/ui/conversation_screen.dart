import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../data/models/assistant.dart';
import '../domain/conversation_actions.dart';
import '../domain/conversations_controller.dart';
import '../domain/decisions.dart';
import 'decision_cards.dart';
import 'status_banner.dart';
import 'status_chip.dart';

/// One conversation: what was said, what waits for the person, and the composer.
class ConversationScreen extends StatefulWidget {
  const ConversationScreen({super.key, required this.requestId});

  final String requestId;

  @override
  State<ConversationScreen> createState() => _ConversationScreenState();
}

class _ConversationScreenState extends State<ConversationScreen> {
  final _scroll = ScrollController();
  int _lastCount = -1;

  @override
  void initState() {
    super.initState();
    // Drop an error left by an earlier screen.
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (mounted) context.read<ConversationActions>().clearError();
    });
  }

  @override
  void dispose() {
    _scroll.dispose();
    super.dispose();
  }

  void _followBottom(int count) {
    if (count == _lastCount) return;
    _lastCount = count;
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (_scroll.hasClients) _scroll.jumpTo(_scroll.position.maxScrollExtent);
    });
  }

  Future<void> _delete(AssistantRequest request) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Delete this conversation?'),
        content: const Text('This cannot be undone. Your fingerprint is needed.'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(context, false), child: const Text('Cancel')),
          FilledButton(onPressed: () => Navigator.pop(context, true), child: const Text('Delete')),
        ],
      ),
    );
    if (confirmed != true || !mounted) return;
    final navigator = Navigator.of(context);
    final done = await context.read<ConversationActions>().remove(request);
    if (done && mounted) navigator.pop();
  }

  @override
  Widget build(BuildContext context) {
    final controller = context.watch<ConversationsController>();
    final actions = context.watch<ConversationActions>();
    final request = controller.byId(widget.requestId);
    if (request == null) {
      return Scaffold(
        appBar: AppBar(),
        body: Center(
          child: Text(controller.snapshot == null ? 'Loading…' : 'This conversation no longer exists.'),
        ),
      );
    }
    final groups = groupEntries(request.entries);
    _followBottom(request.entries.length + (isDeciding(request) ? 1 : 0));
    return Scaffold(
      appBar: AppBar(
        title: Text(request.title, maxLines: 1, overflow: TextOverflow.ellipsis),
        actions: [
          Padding(
            padding: const EdgeInsets.only(right: 4),
            child: Center(child: StatusChip(status: request.status)),
          ),
          PopupMenuButton<String>(
            onSelected: (value) {
              if (value == 'delete') _delete(request);
              if (value == 'dismiss') actions.ignore(request);
            },
            itemBuilder: (context) => const [
              PopupMenuItem(value: 'dismiss', child: Text('Dismiss')),
              PopupMenuItem(value: 'delete', child: Text('Delete')),
            ],
          ),
        ],
      ),
      body: Column(
        children: [
          LinkBanner(controller: controller),
          Expanded(
            child: ListView(
              controller: _scroll,
              padding: const EdgeInsets.all(12),
              children: [
                for (final group in groups)
                  switch (group) {
                    StepsGroup(:final entries) => _Steps(entries: entries),
                    MessageGroup(:final entry) => _Message(entry: entry),
                  },
                _Footer(request: request, actions: actions),
              ],
            ),
          ),
          _Bottom(request: request, actions: actions),
        ],
      ),
    );
  }
}

/// Consecutive work-log entries become one group; messages stay on their own.
sealed class EntryGroup {
  const EntryGroup();
}

class StepsGroup extends EntryGroup {
  const StepsGroup(this.entries);
  final List<ChatEntry> entries;
}

class MessageGroup extends EntryGroup {
  const MessageGroup(this.entry);
  final ChatEntry entry;
}

List<EntryGroup> groupEntries(List<ChatEntry> entries) {
  final groups = <EntryGroup>[];
  for (final entry in entries) {
    if (entry.kind == EntryKind.step) {
      final last = groups.isEmpty ? null : groups.last;
      if (last is StepsGroup) {
        last.entries.add(entry);
      } else {
        groups.add(StepsGroup([entry]));
      }
    } else {
      groups.add(MessageGroup(entry));
    }
  }
  return groups;
}

class _Message extends StatelessWidget {
  const _Message({required this.entry});

  final ChatEntry entry;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final mine = entry.role == EntryRole.user;
    final system = entry.role == EntryRole.system;
    return Align(
      alignment: mine ? Alignment.centerRight : Alignment.centerLeft,
      child: Container(
        margin: const EdgeInsets.symmetric(vertical: 4),
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 8),
        constraints: BoxConstraints(maxWidth: MediaQuery.sizeOf(context).width * 0.86),
        decoration: BoxDecoration(
          color: mine
              ? scheme.primaryContainer
              : system
              ? scheme.surfaceContainerHighest
              : scheme.surfaceContainerHigh,
          borderRadius: BorderRadius.circular(14),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            SelectableText(entry.text),
            for (final attachment in entry.attachments)
              Padding(
                padding: const EdgeInsets.only(top: 4),
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    const Icon(Icons.attach_file, size: 14),
                    const SizedBox(width: 4),
                    Flexible(child: Text(attachment.name, style: Theme.of(context).textTheme.bodySmall)),
                  ],
                ),
              ),
          ],
        ),
      ),
    );
  }
}

class _Steps extends StatelessWidget {
  const _Steps({required this.entries});

  final List<ChatEntry> entries;

  @override
  Widget build(BuildContext context) {
    return ExpansionTile(
      tilePadding: EdgeInsets.zero,
      dense: true,
      shape: const Border(),
      collapsedShape: const Border(),
      leading: const Icon(Icons.timeline, size: 18),
      title: Text(
        '${entries.length} ${entries.length == 1 ? 'step' : 'steps'}',
        style: Theme.of(context).textTheme.bodySmall,
      ),
      children: [
        for (final entry in entries)
          Align(
            alignment: Alignment.centerLeft,
            child: Padding(
              padding: const EdgeInsets.only(bottom: 4),
              child: Text(entry.text, style: Theme.of(context).textTheme.bodySmall),
            ),
          ),
      ],
    );
  }
}

/// Below the entries: that the assistant is working, or why it failed.
class _Footer extends StatelessWidget {
  const _Footer({required this.request, required this.actions});

  final AssistantRequest request;
  final ConversationActions actions;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    if (request.isWorking) {
      return Padding(
        padding: const EdgeInsets.symmetric(vertical: 12),
        child: Row(
          children: [
            const SizedBox(width: 16, height: 16, child: CircularProgressIndicator(strokeWidth: 2)),
            const SizedBox(width: 12),
            const Expanded(child: Text('Working…')),
            OutlinedButton(
              onPressed: actions.busy ? null : () => actions.stop(request),
              child: const Text('Stop'),
            ),
          ],
        ),
      );
    }
    if (request.status == AssistantStatus.failed) {
      return Container(
        margin: const EdgeInsets.symmetric(vertical: 8),
        padding: const EdgeInsets.all(12),
        decoration: BoxDecoration(color: scheme.errorContainer, borderRadius: BorderRadius.circular(10)),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              request.error?.isNotEmpty == true ? request.error! : 'Something went wrong.',
              style: TextStyle(color: scheme.onErrorContainer),
            ),
            const SizedBox(height: 8),
            FilledButton.tonal(
              onPressed: actions.busy ? null : () => actions.retry(request),
              child: const Text('Investigate again'),
            ),
          ],
        ),
      );
    }
    return const SizedBox.shrink();
  }
}

class _Bottom extends StatelessWidget {
  const _Bottom({required this.request, required this.actions});

  final AssistantRequest request;
  final ConversationActions actions;

  @override
  Widget build(BuildContext context) {
    if (isDeciding(request)) {
      return switch (pendingDecision(request)!) {
        Decision.questions => QuestionsCard(
          key: ValueKey('q-${request.id}-${request.updatedAt}'),
          request: request,
          actions: actions,
        ),
        Decision.plan => PlanCard(key: ValueKey('p-${request.plan?.id}'), request: request, actions: actions),
        Decision.proposal => ProposalCard(
          key: ValueKey('r-${request.proposal?.id}'),
          request: request,
          actions: actions,
        ),
      };
    }
    if (!canWrite(request)) return const SizedBox.shrink();
    return Composer(request: request, actions: actions);
  }
}

/// The follow-up field.
class Composer extends StatefulWidget {
  const Composer({super.key, required this.request, required this.actions});

  final AssistantRequest request;
  final ConversationActions actions;

  @override
  State<Composer> createState() => _ComposerState();
}

class _ComposerState extends State<Composer> {
  final _text = TextEditingController();

  @override
  void dispose() {
    _text.dispose();
    super.dispose();
  }

  Future<void> _send() async {
    final text = _text.text.trim();
    if (text.isEmpty) return;
    if (await widget.actions.send(widget.request, text)) _text.clear();
  }

  @override
  Widget build(BuildContext context) {
    final busy = widget.actions.busy;
    final error = widget.actions.error;
    return Material(
      color: Theme.of(context).colorScheme.surfaceContainerLow,
      child: SafeArea(
        top: false,
        child: Padding(
          padding: const EdgeInsets.fromLTRB(12, 8, 8, 8),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              if (error != null)
                Padding(
                  padding: const EdgeInsets.only(bottom: 6),
                  child: Align(
                    alignment: Alignment.centerLeft,
                    child: Text(error, style: TextStyle(color: Theme.of(context).colorScheme.error)),
                  ),
                ),
              Row(
                crossAxisAlignment: CrossAxisAlignment.end,
                children: [
                  Expanded(
                    child: TextField(
                      controller: _text,
                      enabled: !busy,
                      minLines: 1,
                      maxLines: 5,
                      textCapitalization: TextCapitalization.sentences,
                      decoration: InputDecoration(
                        hintText: widget.request.hasGrants
                            ? 'Follow up (needs your fingerprint)'
                            : 'Follow up',
                        isDense: true,
                      ),
                      onChanged: (_) => setState(() {}),
                    ),
                  ),
                  IconButton.filled(
                    key: const Key('send-message'),
                    onPressed: busy || _text.text.trim().isEmpty ? null : _send,
                    icon: Icon(widget.request.hasGrants ? Icons.fingerprint : Icons.send),
                  ),
                ],
              ),
            ],
          ),
        ),
      ),
    );
  }
}
