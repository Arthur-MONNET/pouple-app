import 'dart:async';

import 'package:pouple_companion/src/data/net/assistant_api.dart';
import 'package:pouple_companion/src/data/net/remote_failure.dart';
import 'package:pouple_companion/src/data/stepup/step_up_signer.dart';

/// An [AssistantApi] that records its calls instead of talking to a Mac.
// ignore: avoid_implementing_value_types
class FakeApi implements AssistantApi {
  final List<String> calls = [];
  RemoteFailure? failure;
  SignerException? signerFailure;
  Completer<void>? _held;

  void hold() => _held = Completer<void>();
  void release() => _held?.complete();

  Future<void> _record(String call, {bool signs = false}) async {
    final held = _held;
    if (held != null) await held.future;
    if (failure != null) throw failure!;
    if (signs && signerFailure != null) throw signerFailure!;
    calls.add(call);
  }

  @override
  Future<void> sendMessage(String id, String text, {required bool hasGrants}) =>
      _record('message $id $text grants=$hasGrants');

  @override
  Future<void> answer(String id, List<QuestionAnswerInput> answers, {required bool hasGrants}) => _record(
    'answer ${answers.map((a) => '${a.questionId}:${a.optionId ?? a.optionIds ?? 'text(${a.text})'}').join(' ')}',
  );

  @override
  Future<void> decideProposal(String id, {required String proposalId, required bool approve}) =>
      _record('proposal $id $proposalId ${approve ? 'approve' : 'decline'}', signs: approve);

  @override
  Future<void> decidePlan(String id, {required String planId, required bool approve, List<int>? actions}) =>
      _record('plan $id $planId ${approve ? 'approve' : 'decline'} $actions', signs: approve);

  @override
  Future<void> stop(String id) => _record('stop $id');

  @override
  Future<void> retry(String id) => _record('retry $id');

  @override
  Future<void> ignore(String id) => _record('ignore $id');

  @override
  Future<void> close(String id, {String? updatedAt}) => _record('close $id');

  @override
  Future<void> remove(String id) => _record('remove $id', signs: true);

  @override
  Future<String> createConversation(String text, {String? projectId, required bool needsStepUp}) async {
    await _record('create $text $projectId');
    return 'new0001';
  }

  @override
  dynamic noSuchMethod(Invocation invocation) => super.noSuchMethod(invocation);
}
