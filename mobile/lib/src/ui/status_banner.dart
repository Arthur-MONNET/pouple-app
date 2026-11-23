import 'package:flutter/material.dart';

import '../data/net/remote_failure.dart';
import '../domain/conversations_controller.dart';

/// "12:04" in the phone's local time.
String clock(DateTime time) {
  final local = time.toLocal();
  String two(int value) => value.toString().padLeft(2, '0');
  return '${two(local.hour)}:${two(local.minute)}';
}

/// Tells that the Mac cannot be reached, since when, and why when it is not just asleep.
class LinkBanner extends StatelessWidget {
  const LinkBanner({super.key, required this.controller, this.onRepair, this.onUpdateAddress});

  final ConversationsController controller;

  /// Offered when the certificate changed: the only fix is to pair again.
  final VoidCallback? onRepair;

  /// Offered when nothing answered: the Mac's Internet address may have changed.
  final VoidCallback? onUpdateAddress;

  @override
  Widget build(BuildContext context) {
    if (controller.link != LinkState.unreachable) return const SizedBox.shrink();
    final since = controller.unreachableSince;
    final mismatch = controller.failure?.kind == FailureKind.certificateMismatch;
    final scheme = Theme.of(context).colorScheme;
    final base = since == null ? 'Mac unreachable' : 'Mac unreachable since ${clock(since)}';
    final text = mismatch
        ? 'This is not the Mac that was paired: its certificate changed.'
        : onUpdateAddress == null
        ? base
        : "$base. Its address may have changed.";
    return Material(
      color: scheme.errorContainer,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
        child: Row(
          children: [
            Icon(Icons.cloud_off, size: 18, color: scheme.onErrorContainer),
            const SizedBox(width: 10),
            Expanded(
              child: Text(text, style: TextStyle(color: scheme.onErrorContainer)),
            ),
            if (mismatch && onRepair != null)
              TextButton(onPressed: onRepair, child: const Text('Pair again')),
            if (!mismatch && onUpdateAddress != null)
              TextButton(onPressed: onUpdateAddress, child: const Text('Update address')),
          ],
        ),
      ),
    );
  }
}
