import 'package:flutter/material.dart';

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

class StatusChip extends StatelessWidget {
  const StatusChip({super.key, required this.status});

  final AssistantStatus status;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    final (background, foreground) = switch (status) {
      AssistantStatus.awaitingUser => (scheme.primary, scheme.onPrimary),
      AssistantStatus.failed => (scheme.errorContainer, scheme.onErrorContainer),
      AssistantStatus.queued ||
      AssistantStatus.analyzing ||
      AssistantStatus.applying => (scheme.tertiaryContainer, scheme.onTertiaryContainer),
      _ => (scheme.surfaceContainerHighest, scheme.onSurfaceVariant),
    };
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
      decoration: BoxDecoration(color: background, borderRadius: BorderRadius.circular(10)),
      child: Text(statusLabel(status), style: TextStyle(fontSize: 12, color: foreground)),
    );
  }
}
