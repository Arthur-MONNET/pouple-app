import 'dart:convert';
import 'dart:typed_data';

import '../stepup/canonical_json.dart';
import '../stepup/step_up_signer.dart';
import 'remote_client.dart';
import 'remote_failure.dart';

/// What the person sees on the fingerprint prompt.
class ApprovalPrompt {
  const ApprovalPrompt(this.title, this.subtitle);

  final String title;
  final String subtitle;
}

/// A single answer to a question of the assistant.
class QuestionAnswerInput {
  const QuestionAnswerInput({required this.questionId, this.optionId, this.optionIds, this.text});

  final String questionId;
  final String? optionId;
  final List<String>? optionIds;
  final String? text;

  Map<String, Object?> toJson() => {
    'questionId': questionId,
    'optionId': ?optionId,
    'optionIds': ?optionIds,
    if (text != null && text!.isNotEmpty) 'text': text,
  };
}

/// The conversation actions of the phone, with the fingerprint step-up the Mac asks for on the sensitive ones.
///
/// A step-up signs the exact bytes of the request body, so the body is built once, hashed, signed and sent as is.
class AssistantApi {
  AssistantApi({required this.client, required this.signer, required this.deviceId});

  final RemoteClient client;
  final StepUpSigner signer;
  final String deviceId;

  static String _path(String id, String action) =>
      '/api/assistant/requests/${Uri.encodeComponent(id)}/$action';

  Future<void> sendMessage(String id, String text, {required bool hasGrants}) => _post(
    _path(id, 'messages'),
    StepUpRoute.messages,
    id,
    {'text': text, 'attachmentIds': const <String>[]},
    stepUp: hasGrants,
    prompt: const ApprovalPrompt('Send message', 'This conversation can write on its own.'),
  );

  Future<void> answer(String id, List<QuestionAnswerInput> answers, {required bool hasGrants}) => _post(
    _path(id, 'answers'),
    StepUpRoute.answers,
    id,
    {
      'answers': [for (final answer in answers) answer.toJson()],
    },
    stepUp: hasGrants,
    prompt: const ApprovalPrompt('Send answer', 'This conversation can write on its own.'),
  );

  Future<void> decideProposal(String id, {required String proposalId, required bool approve}) => _post(
    _path(id, 'proposal'),
    StepUpRoute.proposal,
    id,
    {'decision': approve ? 'approve' : 'decline', 'proposalId': proposalId},
    stepUp: approve,
    prompt: const ApprovalPrompt('Approve change', 'The assistant will write this change.'),
  );

  /// [actions] are the positions ticked on a plan with several actions; null approves the plan as a whole.
  Future<void> decidePlan(String id, {required String planId, required bool approve, List<int>? actions}) =>
      _post(
        _path(id, 'plan'),
        StepUpRoute.plan,
        id,
        {
          'decision': approve ? 'approve' : 'decline',
          'planId': planId,
          if (approve && actions != null) 'actions': actions,
        },
        stepUp: approve,
        prompt: const ApprovalPrompt('Approve plan', 'The assistant will write what the plan says.'),
      );

  Future<void> stop(String id) => _post(_path(id, 'stop'), null, id, null);

  Future<void> retry(String id) => _post(_path(id, 'retry'), null, id, null);

  Future<void> ignore(String id) => _post(_path(id, 'ignore'), null, id, null);

  Future<void> close(String id, {String? updatedAt}) =>
      _post(_path(id, 'close'), null, id, updatedAt == null ? null : {'updatedAt': updatedAt});

  Future<void> remove(String id) async {
    await _withStepUp(
      'DELETE',
      '/api/assistant/requests/${Uri.encodeComponent(id)}',
      StepUpRoute.remove,
      id,
      null,
      const ApprovalPrompt('Delete conversation', 'This cannot be undone.'),
    );
  }

  /// Starts a conversation and returns its id.
  Future<String> createConversation(String text, {String? projectId, required bool needsStepUp}) async {
    final body = <String, Object?>{
      'text': text,
      'attachmentIds': const <String>[],
      if (projectId != null && projectId.isNotEmpty) 'projectId': projectId,
    };
    final response = await _post(
      '/api/assistant/requests',
      StepUpRoute.newConversation,
      '',
      body,
      stepUp: needsStepUp,
      prompt: const ApprovalPrompt('Start conversation', 'Every change applies without asking.'),
    );
    final decoded = response.json;
    if (decoded is Map && decoded['id'] is String) return decoded['id'] as String;
    throw const RemoteFailure(FailureKind.server);
  }

  /// POST [path]. When the Mac says a step-up is needed although it was not expected (the "apply every change"
  /// setting is not in the snapshot), retries once with one.
  Future<RemoteResponse> _post(
    String path,
    String? route,
    String requestId,
    Map<String, Object?>? body, {
    bool stepUp = false,
    ApprovalPrompt? prompt,
  }) async {
    if (route == null) {
      return client.request('POST', path, body: _encode(body));
    }
    final approval = prompt ?? const ApprovalPrompt('Confirm', 'Confirm with your fingerprint.');
    if (stepUp) return _withStepUp('POST', path, route, requestId, body, approval);
    try {
      return await client.request('POST', path, body: _encode(body));
    } on RemoteFailure catch (failure) {
      if (failure.kind != FailureKind.stepUpRequired) rethrow;
      return _withStepUp('POST', path, route, requestId, body, approval);
    }
  }

  Uint8List? _encode(Map<String, Object?>? body) =>
      body == null ? null : Uint8List.fromList(utf8.encode(jsonEncode(body)));

  Future<RemoteResponse> _withStepUp(
    String method,
    String path,
    String route,
    String requestId,
    Map<String, Object?>? body,
    ApprovalPrompt prompt,
  ) async {
    final bytes = _encode(body);
    final planId = body?['planId'];
    final proposalId = body?['proposalId'];
    final actions = body?['actions'];
    final nonce = await client.challenge();
    final canonical = canonicalStepUp(
      nonce: nonce,
      deviceId: deviceId,
      route: route,
      requestId: requestId,
      planId: planId is String ? planId : '',
      proposalId: proposalId is String ? proposalId : '',
      actions: actions is List ? actions.cast<int>() : null,
      bodySha256: bodySha256(bytes ?? const <int>[]),
    );
    final signature = await signer.sign(canonical, title: prompt.title, subtitle: prompt.subtitle);
    return client.request(
      method,
      path,
      body: bytes,
      stepUp: StepUpHeaders(nonce: nonce, signature: signature),
    );
  }
}
