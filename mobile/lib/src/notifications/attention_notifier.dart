import 'package:flutter_local_notifications/flutter_local_notifications.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../domain/decisions.dart';

/// The text of the notification. It never carries a conversation's title or content: the lock screen is public.
String attentionText(int count) => count == 1 ? 'A conversation needs you' : '$count conversations need you';

/// Local notifications for conversations that need the person. There is no push service: the app follows the Mac's
/// stream while it is open, and a periodic background check looks every 15 minutes while it is not.
class AttentionNotifier {
  AttentionNotifier({FlutterLocalNotificationsPlugin? plugin})
    : _plugin = plugin ?? FlutterLocalNotificationsPlugin();

  static const _notifiedKey = 'attention.notified.v1';
  static const _notificationId = 1;
  static const _channelId = 'attention';

  final FlutterLocalNotificationsPlugin _plugin;
  bool _ready = false;

  /// Prepares the plugin (works in the background isolate too).
  Future<void> init() async {
    if (_ready) return;
    await _plugin.initialize(
      settings: const InitializationSettings(android: AndroidInitializationSettings('@mipmap/ic_launcher')),
    );
    _ready = true;
  }

  /// Asks for the notification permission (Android 13 and later). Returns whether it is granted.
  Future<bool> requestPermission() async {
    await init();
    final android = _plugin.resolvePlatformSpecificImplementation<AndroidFlutterLocalNotificationsPlugin>();
    return await android?.requestNotificationsPermission() ?? false;
  }

  /// Records what the person has already seen (the app is open on it), so it is never notified later.
  Future<void> markSeen(Set<String> current) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setStringList(_notifiedKey, current.toList());
  }

  /// Notifies when a conversation needs the person that was not announced yet, and remembers the current ones.
  /// Returns how many new ones there were.
  Future<int> notifyIfNew(Set<String> current) async {
    final prefs = await SharedPreferences.getInstance();
    final notified = (prefs.getStringList(_notifiedKey) ?? const <String>[]).toSet();
    final fresh = newAttention(notified, current);
    await prefs.setStringList(_notifiedKey, current.toList());
    if (fresh.isEmpty) return 0;
    await init();
    await _plugin.show(
      id: _notificationId,
      title: 'Pouple',
      body: attentionText(current.length),
      notificationDetails: const NotificationDetails(
        android: AndroidNotificationDetails(
          _channelId,
          'Conversations that need you',
          channelDescription: 'Tells you when the assistant is waiting for your decision.',
          importance: Importance.high,
          priority: Priority.high,
          visibility: NotificationVisibility.private,
        ),
      ),
    );
    return fresh.length;
  }

  /// Removes what is shown and forgotten (unpairing).
  Future<void> reset() async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.remove(_notifiedKey);
    await init();
    await _plugin.cancelAll();
  }
}
