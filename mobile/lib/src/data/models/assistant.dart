import 'json_reader.dart';

/// Where a conversation stands. A value this app does not know decodes to [unknown] and is shown as a plain,
/// non-actionable state.
enum AssistantStatus {
  queued('queued'),
  analyzing('analyzing'),
  awaitingUser('awaiting-user'),
  ready('ready'),
  applying('applying'),
  done('done'),
  ignored('ignored'),
  failed('failed'),
  unknown('');

  const AssistantStatus(this.wire);

  /// The value on the wire.
  final String wire;

  static AssistantStatus parse(String value) =>
      values.firstWhere((status) => status.wire == value && value.isNotEmpty, orElse: () => unknown);
}

/// Where a plan stands.
enum PlanStatus {
  proposed('proposed'),
  approved('approved'),
  declined('declined'),
  unknown('');

  const PlanStatus(this.wire);
  final String wire;

  static PlanStatus parse(String value) =>
      values.firstWhere((status) => status.wire == value && value.isNotEmpty, orElse: () => unknown);
}

/// Who wrote an entry of a conversation.
enum EntryRole {
  system,
  assistant,
  user,
  unknown;

  static EntryRole parse(String value) =>
      values.firstWhere((role) => role.name == value, orElse: () => unknown);
}

/// A message or a line of the assistant's work log.
enum EntryKind {
  text,
  step,
  unknown;

  static EntryKind parse(String value) =>
      values.firstWhere((kind) => kind.name == value, orElse: () => unknown);
}

/// How a command run by the assistant ended.
enum CommandStatus {
  running,
  completed,
  failed,
  rejected,
  unknown;

  static CommandStatus parse(String value) =>
      values.firstWhere((status) => status.name == value, orElse: () => unknown);
}

/// Origins where nobody wrote: a service reports something.
const _eventOrigins = {'pipeline', 'asana', 'merge-request', 'reminder', 'pull-request'};

/// Where a request came from.
class RequestSource {
  const RequestSource({required this.origin, required this.senderName, this.channelName, this.label});

  factory RequestSource.fromJson(JsonObject json) => RequestSource(
    origin: json.string('origin'),
    senderName: json.stringOr('senderName', ''),
    channelName: json.optString('channelName'),
    label: json.optString('label'),
  );

  /// The raw origin ("in-app", "mention", "pipeline"...). Kept as text: new origins appear with new connectors.
  final String origin;
  final String senderName;
  final String? channelName;

  /// For a conversation started in Pouple: its first line.
  final String? label;

  /// An event of a service: its text is what the service reports.
  bool get isEvent => _eventOrigins.contains(origin);
}

class AssistantOption {
  const AssistantOption({required this.id, required this.label, this.description});

  factory AssistantOption.fromJson(JsonObject json) => AssistantOption(
    id: json.string('id'),
    label: json.string('label'),
    description: json.optString('description'),
  );

  final String id;
  final String label;
  final String? description;
}

class AssistantQuestion {
  const AssistantQuestion({
    required this.id,
    required this.text,
    required this.options,
    this.multiple = false,
  });

  factory AssistantQuestion.fromJson(JsonObject json) => AssistantQuestion(
    id: json.string('id'),
    text: json.string('text'),
    options: json.objects('options').map(AssistantOption.fromJson).toList(growable: false),
    multiple: json.boolOr('multiple', false),
  );

  final String id;
  final String text;
  final List<AssistantOption> options;

  /// Several options can be picked together.
  final bool multiple;
}

/// A change the assistant wants to make; nothing is written before the user approves it.
class AssistantProposal {
  const AssistantProposal({
    required this.id,
    required this.title,
    required this.description,
    required this.projectId,
  });

  factory AssistantProposal.fromJson(JsonObject json) => AssistantProposal(
    id: json.stringOr('id', ''),
    title: json.string('title'),
    description: json.stringOr('description', ''),
    projectId: json.stringOr('projectId', ''),
  );

  /// Names this exact proposal: a decision carries it.
  final String id;
  final String title;
  final String description;
  final String projectId;
}

/// A place a write approval covers.
class PlanScope {
  const PlanScope({required this.service, required this.id, this.name});

  factory PlanScope.fromJson(JsonObject json) =>
      PlanScope(service: json.string('service'), id: json.string('id'), name: json.optString('name'));

  final String service;
  final String id;
  final String? name;
}

/// One write a plan asks for, in the words of the assistant that investigated.
class PlanAction {
  const PlanAction({
    required this.kind,
    required this.scope,
    required this.summary,
    this.branch,
    this.base,
    this.title,
    this.description,
  });

  factory PlanAction.fromJson(JsonObject json) => PlanAction(
    kind: json.string('kind'),
    scope: PlanScope.fromJson(json.object('scope')),
    summary: json.stringOr('summary', ''),
    branch: json.optString('branch'),
    base: json.optString('base'),
    title: json.optString('title'),
    description: json.optString('description'),
  );

  /// "code.edit", "code.sync", "gitlab.merge-request", "slite.write", "terminal.run"... kept as text so a new kind
  /// still shows.
  final String kind;
  final PlanScope scope;
  final String summary;
  final String? branch;
  final String? base;
  final String? title;
  final String? description;
}

/// What the assistant wants to write, for the user to approve before anything is written.
class AssistantPlan {
  const AssistantPlan({
    required this.id,
    required this.goal,
    required this.brief,
    required this.actions,
    required this.status,
  });

  factory AssistantPlan.fromJson(JsonObject json) => AssistantPlan(
    id: json.stringOr('id', ''),
    goal: json.string('goal'),
    brief: json.stringOr('brief', ''),
    actions: json.objects('actions').map(PlanAction.fromJson).toList(growable: false),
    status: PlanStatus.parse(json.stringOr('status', '')),
  );

  /// Names this exact plan: an approval carries it.
  final String id;
  final String goal;
  final String brief;
  final List<PlanAction> actions;
  final PlanStatus status;
}

/// A file that came with a message.
class AttachmentRef {
  const AttachmentRef({required this.id, required this.name, required this.kind});

  factory AttachmentRef.fromJson(JsonObject json) =>
      AttachmentRef(id: json.string('id'), name: json.string('name'), kind: json.stringOr('kind', 'other'));

  final String id;
  final String name;

  /// "image", "text" or "other".
  final String kind;
}

/// A command the assistant ran, shown with the work log.
class CommandDetail {
  const CommandDetail({required this.status, this.command});

  factory CommandDetail.fromJson(JsonObject json) => CommandDetail(
    status: CommandStatus.parse(json.stringOr('status', '')),
    command: json.optString('command'),
  );

  final CommandStatus status;
  final String? command;
}

/// One entry of a conversation.
class ChatEntry {
  const ChatEntry({
    required this.id,
    required this.role,
    required this.kind,
    required this.text,
    this.attachments = const [],
    this.command,
  });

  factory ChatEntry.fromJson(JsonObject json) => ChatEntry(
    id: json.string('id'),
    role: EntryRole.parse(json.string('role')),
    kind: EntryKind.parse(json.string('kind')),
    text: json.stringOr('text', ''),
    attachments: json.objects('attachments').map(AttachmentRef.fromJson).toList(growable: false),
    command: json.optObject('command') == null ? null : CommandDetail.fromJson(json.object('command')),
  );

  final String id;
  final EntryRole role;
  final EntryKind kind;
  final String text;
  final List<AttachmentRef> attachments;
  final CommandDetail? command;
}

/// A conversation as the Mac lists it.
class AssistantRequest {
  const AssistantRequest({
    required this.id,
    required this.source,
    required this.text,
    required this.updatedAt,
    required this.status,
    required this.questions,
    required this.entries,
    this.summary,
    this.proposal,
    this.plan,
    this.grantCount = 0,
    this.error,
    this.continuable = false,
    this.closed = false,
    this.projectId,
  });

  factory AssistantRequest.fromJson(JsonObject json) => AssistantRequest(
    id: json.string('id'),
    source: RequestSource.fromJson(json.object('source')),
    text: json.stringOr('text', ''),
    updatedAt: json.stringOr('updatedAt', ''),
    status: AssistantStatus.parse(json.string('status')),
    summary: json.optString('summary'),
    questions: json.objects('questions').map(AssistantQuestion.fromJson).toList(growable: false),
    proposal: json.optObject('proposal') == null ? null : AssistantProposal.fromJson(json.object('proposal')),
    plan: json.optObject('plan') == null ? null : AssistantPlan.fromJson(json.object('plan')),
    grantCount: json.listLength('grants'),
    entries: json.objects('entries').map(ChatEntry.fromJson).toList(growable: false),
    error: json.optString('error'),
    continuable: json.boolOr('continuable', false),
    closed: json.boolOr('closed', false),
    projectId: json.optString('projectId'),
  );

  final String id;
  final RequestSource source;
  final String text;

  /// RFC 3339 (UTC, milliseconds): sorts correctly as text.
  final String updatedAt;
  final AssistantStatus status;
  final String? summary;
  final List<AssistantQuestion> questions;
  final AssistantProposal? proposal;
  final AssistantPlan? plan;

  /// How many write approvals the conversation holds. Text typed there may lead to writes, so it needs a step-up.
  final int grantCount;
  final List<ChatEntry> entries;
  final String? error;
  final bool continuable;
  final bool closed;
  final String? projectId;

  /// The conversation holds write approvals.
  bool get hasGrants => grantCount > 0;

  /// Whether the assistant is at work.
  bool get isWorking =>
      status == AssistantStatus.queued ||
      status == AssistantStatus.analyzing ||
      status == AssistantStatus.applying;

  /// What the list shows as the title: the label or first line, as the web does.
  String get title {
    String headline(String value) => value.split('\n').first.trim();
    if (source.isEvent) return headline(text);
    if (source.origin == 'in-app') {
      final label = source.label?.trim() ?? '';
      if (label.isNotEmpty) return label;
      final line = headline(text);
      return line.isEmpty ? 'New conversation' : line;
    }
    final sender = source.senderName.isEmpty ? 'Someone' : source.senderName;
    final channel = source.channelName;
    return channel == null ? sender : '$sender in #$channel';
  }
}

/// The assistant's own health, as the Mac reports it.
class AssistantStatusInfo {
  const AssistantStatusInfo({required this.active, this.problem});

  factory AssistantStatusInfo.fromJson(JsonObject json) =>
      AssistantStatusInfo(active: json.boolOr('active', false), problem: json.optString('problem'));

  /// The assistant runs: enabled, with an AI connected.
  final bool active;

  /// Why it is not running, or the last problem met.
  final String? problem;
}

/// GET /api/assistant, and the payload of every snapshot event of the stream.
class AssistantSnapshot {
  const AssistantSnapshot({required this.status, required this.requests});

  factory AssistantSnapshot.fromJson(Object? value) {
    final json = JsonObject.of(value);
    return AssistantSnapshot(
      status: json.optObject('status') == null
          ? const AssistantStatusInfo(active: false)
          : AssistantStatusInfo.fromJson(json.object('status')),
      requests: json.objects('requests').map(AssistantRequest.fromJson).toList(growable: false),
    );
  }

  final AssistantStatusInfo status;
  final List<AssistantRequest> requests;

  AssistantRequest? byId(String id) {
    for (final request in requests) {
      if (request.id == id) return request;
    }
    return null;
  }
}

/// A folder of the projects folder (GET /api/assistant/projects).
class ProjectInfo {
  const ProjectInfo({required this.id, required this.name});

  factory ProjectInfo.fromJson(JsonObject json) =>
      ProjectInfo(id: json.string('id'), name: json.stringOr('name', json.string('id')));

  final String id;
  final String name;
}
