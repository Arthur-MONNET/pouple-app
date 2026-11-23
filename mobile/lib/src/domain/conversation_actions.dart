import 'package:flutter/foundation.dart';

import '../data/models/assistant.dart';
import '../data/net/assistant_api.dart';
import '../data/net/remote_failure.dart';
import '../data/stepup/canonical_json.dart';
import '../data/stepup/step_up_signer.dart';
import 'decisions.dart';

/// The actions of the person on conversations, with one error and one busy flag for the screen to show.
class ConversationActions extends ChangeNotifier {
  ConversationActions({required this.api, required this.refresh, required this.onRevoked});

  final AssistantApi api;

  /// Reads the conversations again (after a conflict, or an action).
  final Future<void> Function() refresh;
  final Future<void> Function() onRevoked;

  bool _busy = false;
  String? _error;
  bool _disposed = false;

  bool get busy => _busy;

  /// The last failure, in words; cleared by the next action.
  String? get error => _error;

  void clearError() {
    if (_error == null) return;
    _error = null;
    notifyListeners();
  }

  /// Runs [action], keeps its failure for display, and returns whether it worked.
  Future<bool> run(Future<void> Function() action) async {
    if (_busy) return false;
    _busy = true;
    _error = null;
    notifyListeners();
    try {
      await action();
      return true;
    } on RemoteFailure catch (failure) {
      _error = failure.display;
      if (failure.kind == FailureKind.unauthorized) await onRevoked();
      if (failure.kind == FailureKind.conflict || failure.kind == FailureKind.notFound) await refresh();
      return false;
    } on SignerException catch (problem) {
      // A cancelled prompt is not an error worth a banner.
      _error = problem.problem == SignerProblem.cancelled ? null : problem.display;
      return false;
    } on StepUpFormatException {
      _error = 'This conversation cannot be approved from the phone. Use the Mac.';
      return false;
    } finally {
      _busy = false;
      if (!_disposed) notifyListeners();
    }
  }

  Future<bool> send(AssistantRequest request, String text) async {
    final done = await run(() => api.sendMessage(request.id, text.trim(), hasGrants: request.hasGrants));
    if (done) await refresh();
    return done;
  }

  /// Answers every question of the request. [choices] holds, by question id, the picked option ids (one for a single
  /// choice) and [free] the free text by question id; a question needs one of them.
  Future<bool> answerQuestions(
    AssistantRequest request, {
    required Map<String, Set<String>> choices,
    required Map<String, String> free,
  }) async {
    final answers = <QuestionAnswerInput>[];
    for (final question in request.questions) {
      final picked = choices[question.id] ?? const <String>{};
      final text = (free[question.id] ?? '').trim();
      if (picked.isEmpty && text.isEmpty) continue;
      if (question.multiple) {
        answers.add(
          QuestionAnswerInput(
            questionId: question.id,
            optionIds: question.options
                .where((option) => picked.contains(option.id))
                .map((o) => o.id)
                .toList(),
            text: text,
          ),
        );
      } else if (picked.isNotEmpty) {
        answers.add(QuestionAnswerInput(questionId: question.id, optionId: picked.first, text: text));
      } else {
        answers.add(QuestionAnswerInput(questionId: question.id, text: text));
      }
    }
    if (answers.isEmpty) return false;
    final done = await run(() => api.answer(request.id, answers, hasGrants: request.hasGrants));
    if (done) await refresh();
    return done;
  }

  /// Approves or declines the plan. [ticked] are the option ids of a picked plan; ignored for a single-action plan.
  Future<bool> decidePlan(
    AssistantRequest request, {
    required bool approve,
    Set<String> ticked = const {},
  }) async {
    final plan = request.plan;
    if (plan == null) return false;
    final actions = approve && planIsPicked(request) ? pickedActions(ticked) : null;
    if (approve && actions != null && actions.isEmpty) return false;
    final done = await run(
      () => api.decidePlan(request.id, planId: plan.id, approve: approve, actions: actions),
    );
    if (done) await refresh();
    return done;
  }

  Future<bool> decideProposal(AssistantRequest request, {required bool approve}) async {
    final proposal = request.proposal;
    if (proposal == null) return false;
    final done = await run(() => api.decideProposal(request.id, proposalId: proposal.id, approve: approve));
    if (done) await refresh();
    return done;
  }

  Future<bool> stop(AssistantRequest request) => _then(() => api.stop(request.id));
  Future<bool> retry(AssistantRequest request) => _then(() => api.retry(request.id));
  Future<bool> ignore(AssistantRequest request) => _then(() => api.ignore(request.id));
  Future<bool> remove(AssistantRequest request) => _then(() => api.remove(request.id));

  Future<bool> _then(Future<void> Function() action) async {
    final done = await run(action);
    if (done) await refresh();
    return done;
  }

  /// Starts a conversation and returns its id, or null on failure.
  Future<String?> create(String text, {String? projectId}) async {
    String? id;
    final done = await run(() async {
      id = await api.createConversation(text.trim(), projectId: projectId, needsStepUp: false);
    });
    if (done) await refresh();
    return done ? id : null;
  }

  @override
  void dispose() {
    _disposed = true;
    super.dispose();
  }
}
