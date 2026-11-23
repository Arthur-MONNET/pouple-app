import 'package:flutter/material.dart';

import '../data/models/assistant.dart';
import '../domain/conversation_actions.dart';
import '../domain/decisions.dart';

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
    return _Dock(
      children: [
        Text('Plan awaiting your approval', style: Theme.of(context).textTheme.labelLarge),
        const SizedBox(height: 4),
        Text(plan.goal, style: Theme.of(context).textTheme.titleMedium),
        const SizedBox(height: 8),
        if (picked) ...[
          const Text('Tick what the assistant may do:'),
          for (final option in question.options)
            CheckboxListTile(
              key: ValueKey('plan-${option.id}'),
              contentPadding: EdgeInsets.zero,
              controlAffinity: ListTileControlAffinity.leading,
              value: _ticked.contains(option.id),
              onChanged: busy
                  ? null
                  : (value) =>
                        setState(() => value == true ? _ticked.add(option.id) : _ticked.remove(option.id)),
              title: Text(option.label),
              subtitle: option.description == null || option.description!.isEmpty
                  ? null
                  : Text(option.description!),
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
            title: const Text('What the AI that writes will know'),
            children: [Align(alignment: Alignment.centerLeft, child: Text(plan.brief))],
          ),
        const SizedBox(height: 8),
        FilledButton.icon(
          onPressed: busy || (picked && _ticked.isEmpty)
              ? null
              : () => widget.actions.decidePlan(widget.request, approve: true, ticked: _ticked),
          icon: const Icon(Icons.fingerprint),
          label: Text(picked ? 'Approve ticked actions' : approve.label),
        ),
        if (!picked && approve.description != null)
          Padding(
            padding: const EdgeInsets.only(top: 4),
            child: Text(approve.description!, style: Theme.of(context).textTheme.bodySmall),
          ),
        const SizedBox(height: 8),
        OutlinedButton(
          onPressed: busy ? null : () => widget.actions.decidePlan(widget.request, approve: false),
          child: Text(question.options.last.label),
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
        Text('Change awaiting your approval', style: Theme.of(context).textTheme.labelLarge),
        const SizedBox(height: 4),
        Text(proposal.title, style: Theme.of(context).textTheme.titleMedium),
        if (proposal.projectId.isNotEmpty)
          Text('in ${proposal.projectId}', style: Theme.of(context).textTheme.bodySmall),
        const SizedBox(height: 8),
        if (proposal.description.isNotEmpty) Text(proposal.description),
        const SizedBox(height: 12),
        FilledButton.icon(
          onPressed: busy ? null : () => actions.decideProposal(request, approve: true),
          icon: const Icon(Icons.fingerprint),
          label: Text(question.options.first.label),
        ),
        const SizedBox(height: 8),
        OutlinedButton(
          onPressed: busy ? null : () => actions.decideProposal(request, approve: false),
          child: Text(question.options.last.label),
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
      elevation: 3,
      child: SafeArea(
        top: false,
        child: ConstrainedBox(
          constraints: BoxConstraints(maxHeight: MediaQuery.sizeOf(context).height * 0.62),
          child: SingleChildScrollView(
            padding: const EdgeInsets.fromLTRB(16, 12, 16, 12),
            child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: children),
          ),
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
