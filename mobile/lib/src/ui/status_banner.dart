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
  const LinkBanner({super.key, required this.controller, this.onRepair});

  final ConversationsController controller;

  /// Offered when the certificate changed: the only fix is to pair again.
  final VoidCallback? onRepair;

  @override
  Widget build(BuildContext context) {
    if (controller.link != LinkState.unreachable) return const SizedBox.shrink();
    final since = controller.unreachableSince;
    final mismatch = controller.failure?.kind == FailureKind.certificateMismatch;
    final scheme = Theme.of(context).colorScheme;
    final text = mismatch
        ? 'This is not the Mac that was paired: its certificate changed.'
        : since == null
        ? 'Mac unreachable'
        : 'Mac unreachable since ${clock(since)}';
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
          ],
        ),
      ),
    );
  }
}
