import '../data/models/assistant.dart';

/// The words for a status.
String statusLabel(AssistantStatus status) => switch (status) {
  AssistantStatus.queued => 'Queued',
  AssistantStatus.analyzing => 'Working',
  AssistantStatus.awaitingUser => 'Your call',
  AssistantStatus.ready => 'Ready',
  AssistantStatus.applying => 'Applying',
  AssistantStatus.done => 'Done',
  AssistantStatus.ignored => 'Dismissed',
  AssistantStatus.failed => 'Failed',
  AssistantStatus.unknown => 'Unknown',
};
