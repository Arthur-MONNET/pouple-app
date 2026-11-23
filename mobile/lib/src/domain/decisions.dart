import '../data/models/assistant.dart';

/// A question card shown to the person: the assistant's own, or one made from a plan or a proposal.
class DecisionQuestion {
  const DecisionQuestion({
    required this.id,
    required this.text,
    required this.options,
    this.multiple = false,
  });

  final String id;
  final String text;
  final List<AssistantOption> options;
  final bool multiple;
}

/// What waits for the user at the bottom of a conversation: its questions, then a plan, then a proposed change.
/// Mirrors web/src/lib/requestDecisions.ts.
enum Decision { questions, plan, proposal }

/// What a plan's action is called where the user picks it.
String planActionLabel(PlanAction action) {
  if (action.kind == 'slite.write') {
    return '${action.title ?? 'Page'} · ${action.scope.id}';
  }
  final arrow = action.kind == 'gitlab.merge-request' ? '→' : '←';
  final branch = action.branch;
  final base = action.base;
  final where = branch != null && branch.isNotEmpty
      ? (base != null && base.isNotEmpty ? '$branch $arrow $base' : branch)
      : action.scope.id;
  return '${planKindLabel(action.kind)} · $where';
}

/// The kind of a plan action in words; an unknown kind is shown as it is.
String planKindLabel(String kind) => switch (kind) {
  'slite.write' => 'Create a Slite page under',
  'gitlab.merge-request' => 'Open a merge request',
  'code.edit' => 'Write code',
  'code.sync' => 'Update a branch',
  'terminal.run' => 'Work in the terminal',
  _ => kind,
};

/// A plan with several actions lets the user tick the ones they want: only those are carried out.
bool planIsPicked(AssistantRequest request) => (request.plan?.actions.length ?? 0) > 1;

/// Whether the plan waits for an approval.
bool planAwaitsApproval(AssistantRequest request) {
  final plan = request.plan;
  return plan != null && plan.status == PlanStatus.proposed;
}

/// The yes/no card of a proposal; null when there is no proposal.
DecisionQuestion? proposalQuestion(AssistantRequest request) {
  final proposal = request.proposal;
  if (proposal == null) return null;
  return DecisionQuestion(
    id: 'proposal',
    text: 'Apply this change in ${proposal.projectId}? ${proposal.title}',
    options: [
      AssistantOption(id: 'approve', label: 'Yes, make the change', description: proposal.description),
      const AssistantOption(id: 'decline', label: 'No, leave the code alone'),
    ],
  );
}

/// The card of a plan waiting for approval; null when there is none. A plan with several actions is a multiple
/// choice (ticked actions only are carried out), a single action is a yes/no.
DecisionQuestion? planQuestion(AssistantRequest request) {
  final plan = request.plan;
  if (plan == null || plan.status != PlanStatus.proposed) return null;
  if (planIsPicked(request)) {
    return DecisionQuestion(
      id: 'plan',
      text: 'Which of these should I do? ${plan.goal}',
      multiple: true,
      options: [
        for (var i = 0; i < plan.actions.length; i++)
          AssistantOption(
            id: 'action-$i',
            label: planActionLabel(plan.actions[i]),
            description: plan.actions[i].summary,
          ),
      ],
    );
  }
  final kinds = plan.actions.map((action) => action.kind).toSet();
  final String approval;
  final String help;
  if (kinds.contains('slite.write')) {
    approval = 'Yes, publish the page in Slite';
    help = 'This approval covers the title, Markdown and parent shown above.';
  } else if (kinds.contains('gitlab.merge-request')) {
    approval = 'Yes, open the merge request';
    help = 'Allows only this MR to be created in this project between the displayed branches.';
  } else if (kinds.contains('code.sync')) {
    approval = 'Yes, update the branch and push it';
    help =
        'Pouple fetches from origin, merges the other branch in, settles any conflict, commits and pushes the '
        'branch. Nothing is forced. This approval holds for this conversation only.';
  } else {
    approval = 'Yes, write it, commit it and push the branch';
    help =
        'The code is written in a copy of the project on the new branch, committed, and the branch is pushed. '
        'Nothing is forced. This approval holds for this conversation only.';
  }
  return DecisionQuestion(
    id: 'plan',
    text: 'Approve this plan? ${plan.goal}',
    options: [
      AssistantOption(id: 'approve', label: approval, description: help),
      const AssistantOption(id: 'decline', label: 'No, write nothing'),
    ],
  );
}

/// The positions of the ticked actions of a picked plan, in the plan's order.
List<int> pickedActions(Iterable<String> optionIds) =>
    optionIds.map((id) => int.parse(id.replaceFirst('action-', ''))).toList()..sort();

/// What waits for the user, or null when nothing does.
Decision? pendingDecision(AssistantRequest request) {
  if (request.questions.isNotEmpty) return Decision.questions;
  if (planQuestion(request) != null) return Decision.plan;
  if (request.proposal != null) return Decision.proposal;
  return null;
}

/// Whether a conversation needs the person: a decision is waiting, or it failed. The list's badge counts these.
bool needsYou(AssistantRequest request) =>
    request.status == AssistantStatus.awaitingUser || request.status == AssistantStatus.failed;

/// How many conversations need the person.
int needsYouCount(Iterable<AssistantRequest> requests) => requests.where(needsYou).length;

/// A conversation's decision card is shown only while it awaits the user and something is pending.
bool isDeciding(AssistantRequest request) =>
    request.status == AssistantStatus.awaitingUser && pendingDecision(request) != null;

/// The composer is for follow-ups: not while working, and not while a card above waits for an answer.
bool canWrite(AssistantRequest request) =>
    !request.isWorking && request.status != AssistantStatus.awaitingUser;

/// The conversations the person sees, newest activity first, closed bubbles of finished work hidden never: the phone
/// lists every conversation the Mac lists.
List<AssistantRequest> byRecentActivity(Iterable<AssistantRequest> requests) =>
    [...requests]..sort((a, b) => b.updatedAt.compareTo(a.updatedAt));

/// The keys of the conversations that need the person, by id and last update: a conversation that needs the person
/// again after being handled has a new key.
Set<String> attentionKeys(Iterable<AssistantRequest> requests) => {
  for (final request in requests)
    if (needsYou(request)) '${request.id}@${request.updatedAt}',
};

/// The keys in [current] that were not in [notified].
Set<String> newAttention(Set<String> notified, Set<String> current) => current.difference(notified);
