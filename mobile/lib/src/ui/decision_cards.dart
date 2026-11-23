import 'dart:async';

import 'package:flutter/material.dart';

import '../data/models/assistant.dart';
import '../domain/conversation_actions.dart';
import '../domain/decisions.dart';
import '../domain/session.dart';

import 'package:provider/provider.dart';

import 'theme.dart';

/// The assistant's questions: single or multiple choice, or the person's own words.
class QuestionsCard extends StatefulWidget {
  const QuestionsCard({super.key, required this.request, required this.actions});

  final AssistantRequest request;
  final ConversationActions actions;

  @override
  State<QuestionsCard> createState() => _QuestionsCardState();
}

class _QuestionsCardState extends State<QuestionsCard> {
  final Map<String, Set<String>> _choices = {};
  final Map<String, TextEditingController> _free = {};

  TextEditingController _controller(String id) => _free.putIfAbsent(id, TextEditingController.new);

  @override
  void dispose() {
    for (final controller in _free.values) {
      controller.dispose();
    }
    super.dispose();
  }

  bool get _complete => widget.request.questions.every(
    (question) =>
        (_choices[question.id]?.isNotEmpty ?? false) || _controller(question.id).text.trim().isNotEmpty,
  );

  void _toggle(AssistantQuestion question, String optionId) {
    setState(() {
      final picked = _choices.putIfAbsent(question.id, () => <String>{});
      if (question.multiple) {
        if (!picked.add(optionId)) picked.remove(optionId);
      } else {
        picked
          ..clear()
          ..add(optionId);
      }
    });
  }

  @override
  Widget build(BuildContext context) {
    final busy = widget.actions.busy;
    return _Dock(
      children: [
        for (final question in widget.request.questions) ...[
          Text(question.text, style: Theme.of(context).textTheme.titleMedium),
          const SizedBox(height: 4),
          for (final option in question.options)
            question.multiple
                ? CheckboxListTile(
                    key: ValueKey('option-${option.id}'),
                    contentPadding: EdgeInsets.zero,
                    value: _choices[question.id]?.contains(option.id) ?? false,
                    onChanged: busy ? null : (_) => _toggle(question, option.id),
                    title: Text(option.label),
                    subtitle: option.description == null ? null : Text(option.description!),
                    controlAffinity: ListTileControlAffinity.leading,
                  )
                : ListTile(
                    key: ValueKey('option-${option.id}'),
                    contentPadding: EdgeInsets.zero,
                    leading: Icon(
                      (_choices[question.id]?.contains(option.id) ?? false)
                          ? Icons.radio_button_checked
                          : Icons.radio_button_unchecked,
                    ),
                    title: Text(option.label),
                    subtitle: option.description == null ? null : Text(option.description!),
                    onTap: busy ? null : () => _toggle(question, option.id),
                  ),
          TextField(
            controller: _controller(question.id),
            enabled: !busy,
            decoration: const InputDecoration(hintText: 'Or answer in your own words', isDense: true),
            onChanged: (_) => setState(() {}),
          ),
          const SizedBox(height: 12),
        ],
        FilledButton(
          onPressed: busy || !_complete
              ? null
              : () => widget.actions.answerQuestions(
                  widget.request,
                  choices: _choices,
                  free: {for (final entry in _free.entries) entry.key: entry.value.text},
                ),
          child: const Text('Send answer'),
        ),
        if (widget.request.hasGrants) const _FingerprintNote('Answering here needs your fingerprint.'),
        _ActionError(actions: widget.actions),
      ],
    );
  }
}

/// A plan waiting for approval. Approving needs the fingerprint; declining does not.
class PlanCard extends StatefulWidget {
  const PlanCard({super.key, required this.request, required this.actions});

  final AssistantRequest request;
  final ConversationActions actions;

  @override
  State<PlanCard> createState() => _PlanCardState();
}

class _PlanCardState extends State<PlanCard> {
  final Set<String> _ticked = {};

  @override
  Widget build(BuildContext context) {
    final plan = widget.request.plan!;
    final question = planQuestion(widget.request)!;
    final picked = question.multiple;
    final busy = widget.actions.busy;
    final approve = question.options.first;
    final scheme = Theme.of(context).colorScheme;
    final count = picked ? question.options.length : plan.actions.length;
    final approveLabel = picked
        ? (_ticked.isEmpty
              ? 'Approve actions'
              : 'Approve ${_ticked.length} ${_ticked.length == 1 ? 'action' : 'actions'}')
        : approve.label;
    return _Dock(
      children: [
        Text('PLAN · $count ${count == 1 ? 'action' : 'actions'}', style: sectionLabelStyle(context)),
        const SizedBox(height: 6),
        Text(plan.goal, style: const TextStyle(fontSize: 14.5, fontWeight: FontWeight.w600)),
        const SizedBox(height: 8),
        if (picked) ...[
          for (final option in question.options)
            CheckboxListTile(
              key: ValueKey('plan-${option.id}'),
              contentPadding: EdgeInsets.zero,
              controlAffinity: ListTileControlAffinity.leading,
              activeColor: scheme.primary,
              checkColor: scheme.onPrimary,
              value: _ticked.contains(option.id),
              onChanged: busy
                  ? null
                  : (value) =>
                        setState(() => value == true ? _ticked.add(option.id) : _ticked.remove(option.id)),
              title: Text(option.label, style: const TextStyle(fontSize: 13.5, fontWeight: FontWeight.w600)),
              subtitle: option.description == null || option.description!.isEmpty
                  ? null
                  : Text(option.description!, style: TextStyle(color: scheme.onSurfaceVariant)),
            ),
        ] else
          for (final action in plan.actions)
            ListTile(
              contentPadding: EdgeInsets.zero,
              dense: true,
              leading: const Icon(Icons.edit_note),
              title: Text(planActionLabel(action)),
              subtitle: action.summary.isEmpty ? null : Text(action.summary),
            ),
        if (plan.brief.isNotEmpty)
          ExpansionTile(
            tilePadding: EdgeInsets.zero,
            shape: const Border(),
            collapsedShape: const Border(),
            title: const Text('What the AI that writes will know'),
            children: [Align(alignment: Alignment.centerLeft, child: Text(plan.brief))],
          ),
        const SizedBox(height: 8),
        FilledButton(
          onPressed: busy || (picked && _ticked.isEmpty)
              ? null
              : () => withFingerprintSheet(
                  context,
                  () => widget.actions.decidePlan(widget.request, approve: true, ticked: _ticked),
                ),
          child: Text(approveLabel, textAlign: TextAlign.center),
        ),
        if (!picked && approve.description != null)
          Padding(
            padding: const EdgeInsets.only(top: 4),
            child: Text(approve.description!, style: Theme.of(context).textTheme.bodySmall),
          ),
        TextButton(
          style: TextButton.styleFrom(
            foregroundColor: scheme.onSurfaceVariant,
            minimumSize: const Size(64, 48),
          ),
          onPressed: busy ? null : () => widget.actions.decidePlan(widget.request, approve: false),
          child: const Text('Decline'),
        ),
        _ActionError(actions: widget.actions),
      ],
    );
  }
}

/// A change the assistant proposes. Approving needs the fingerprint; declining does not.
class ProposalCard extends StatelessWidget {
  const ProposalCard({super.key, required this.request, required this.actions});

  final AssistantRequest request;
  final ConversationActions actions;

  @override
  Widget build(BuildContext context) {
    final proposal = request.proposal!;
    final question = proposalQuestion(request)!;
    final busy = actions.busy;
    return _Dock(
      children: [
        Text('CHANGE AWAITING YOUR APPROVAL', style: sectionLabelStyle(context)),
        const SizedBox(height: 4),
        Text(proposal.title, style: Theme.of(context).textTheme.titleMedium),
        if (proposal.projectId.isNotEmpty)
          Text('in ${proposal.projectId}', style: Theme.of(context).textTheme.bodySmall),
        const SizedBox(height: 8),
        if (proposal.description.isNotEmpty) Text(proposal.description),
        const SizedBox(height: 12),
        FilledButton(
          onPressed: busy
              ? null
              : () => withFingerprintSheet(context, () => actions.decideProposal(request, approve: true)),
          child: Text(question.options.first.label, textAlign: TextAlign.center),
        ),
        TextButton(
          style: TextButton.styleFrom(
            foregroundColor: Theme.of(context).colorScheme.onSurfaceVariant,
            minimumSize: const Size(64, 48),
          ),
          onPressed: busy ? null : () => actions.decideProposal(request, approve: false),
          child: const Text('Decline'),
        ),
        _ActionError(actions: actions),
      ],
    );
  }
}

class _Dock extends StatelessWidget {
  const _Dock({required this.children});

  final List<Widget> children;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Material(
      color: scheme.surfaceContainerLow,
      shape: RoundedRectangleBorder(
        borderRadius: const BorderRadius.vertical(top: Radius.circular(24)),
        side: BorderSide(color: scheme.outlineVariant),
      ),
      child: SafeArea(
        top: false,
        child: ConstrainedBox(
          constraints: BoxConstraints(maxHeight: MediaQuery.sizeOf(context).height * 0.62),
          child: SingleChildScrollView(
            padding: const EdgeInsets.fromLTRB(16, 16, 16, 12),
            child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: children),
          ),
        ),
      ),
    );
  }
}

/// Runs [action] (an approval, which asks for the fingerprint) while a bottom sheet explains the prompt, and closes
/// the sheet when the action is over. The sheet cannot be dismissed by hand: the system prompt is what is answered.
Future<bool> withFingerprintSheet(BuildContext context, Future<bool> Function() action) async {
  final navigator = Navigator.of(context);
  final session = Provider.of<SessionController?>(context, listen: false);
  final mac = session?.credentials?.macName ?? 'your Mac';
  var open = true;
  unawaited(
    showModalBottomSheet<void>(
      context: context,
      isDismissible: false,
      enableDrag: false,
      backgroundColor: Theme.of(context).colorScheme.surfaceContainerHigh,
      shape: const RoundedRectangleBorder(borderRadius: BorderRadius.vertical(top: Radius.circular(24))),
      builder: (context) => _FingerprintSheet(mac: mac),
    ).whenComplete(() => open = false),
  );
  try {
    return await action();
  } finally {
    if (open) navigator.pop();
  }
}

class _FingerprintSheet extends StatelessWidget {
  const _FingerprintSheet({required this.mac});

  final String mac;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(24, 12, 24, 24),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Container(
              width: 36,
              height: 4,
              decoration: BoxDecoration(color: scheme.outline, borderRadius: BorderRadius.circular(999)),
            ),
            const SizedBox(height: 22),
            Container(
              width: 72,
              height: 72,
              decoration: BoxDecoration(
                shape: BoxShape.circle,
                color: scheme.primary.withValues(alpha: 0.14),
              ),
              child: Icon(Icons.fingerprint, size: 38, color: scheme.primary),
            ),
            const SizedBox(height: 16),
            const Text(
              'Confirm with your fingerprint',
              textAlign: TextAlign.center,
              style: TextStyle(fontWeight: FontWeight.w600, fontSize: 20),
            ),
            const SizedBox(height: 6),
            Text(
              'Approving changes on $mac needs you. Touch the fingerprint sensor.',
              textAlign: TextAlign.center,
              style: TextStyle(fontSize: 13.5, height: 1.5, color: scheme.onSurfaceVariant),
            ),
          ],
        ),
      ),
    );
  }
}

class _FingerprintNote extends StatelessWidget {
  const _FingerprintNote(this.text);

  final String text;

  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.only(top: 6),
    child: Row(
      children: [
        const Icon(Icons.fingerprint, size: 16),
        const SizedBox(width: 6),
        Expanded(child: Text(text, style: Theme.of(context).textTheme.bodySmall)),
      ],
    ),
  );
}

class _ActionError extends StatelessWidget {
  const _ActionError({required this.actions});

  final ConversationActions actions;

  @override
  Widget build(BuildContext context) {
    final error = actions.error;
    if (error == null) return const SizedBox.shrink();
    return Padding(
      padding: const EdgeInsets.only(top: 8),
      child: Text(error, style: TextStyle(color: Theme.of(context).colorScheme.error)),
    );
  }
}
