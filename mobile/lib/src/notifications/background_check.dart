import 'package:flutter/foundation.dart';
import 'package:workmanager/workmanager.dart';

import '../data/net/remote_client.dart';
import '../data/net/remote_failure.dart';
import '../data/storage/credentials.dart';
import '../domain/decisions.dart';
import 'attention_notifier.dart';

const _taskName = 'pouple.attention.check';
const _uniqueName = 'pouple-attention-check';

/// Entry point of the background isolate: one read of the conversations, one notification if something new needs the
/// person. The Mac being asleep or away is the normal case and is not an error.
@pragma('vm:entry-point')
void backgroundDispatcher() {
  Workmanager().executeTask((task, inputData) async {
    if (task != _taskName) return true;
    final credentials = await SecureCredentialStore().load();
    if (credentials == null) return true;
    final client = RemoteClient(endpoint: credentials.endpoint, token: credentials.token);
    try {
      final snapshot = await client.snapshot();
      await AttentionNotifier().notifyIfNew(attentionKeys(snapshot.requests));
    } on RemoteFailure {
      // Unreachable, or no longer paired: nothing to say from the background.
    } finally {
      client.close();
    }
    return true;
  });
}

/// Schedules and cancels the 15-minute check.
class BackgroundCheck {
  const BackgroundCheck();

  Future<void> schedule() async {
    try {
      await Workmanager().initialize(backgroundDispatcher);
      await Workmanager().registerPeriodicTask(
        _uniqueName,
        _taskName,
        frequency: const Duration(minutes: 15),
        constraints: Constraints(networkType: NetworkType.connected),
        existingWorkPolicy: ExistingPeriodicWorkPolicy.keep,
      );
    } on Object catch (error) {
      // The check is a convenience: the app works without it.
      debugPrint('Background check not scheduled: ${error.runtimeType}');
    }
  }

  Future<void> cancel() async {
    try {
      await Workmanager().cancelByUniqueName(_uniqueName);
    } on Object catch (error) {
      debugPrint('Background check not cancelled: ${error.runtimeType}');
    }
  }
}
