import 'package:flutter/foundation.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../data/stepup/step_up_signer.dart';

/// The optional lock: when on, the app asks for the fingerprint or the screen lock when it opens, and again when it
/// comes back after [grace] in the background.
class AppLock extends ChangeNotifier {
  AppLock({required this.signer, this.grace = const Duration(seconds: 30), DateTime Function()? now})
    : _now = now ?? DateTime.now;

  static const _prefKey = 'appLock.enabled';

  final StepUpSigner signer;
  final Duration grace;
  final DateTime Function() _now;

  bool _enabled = false;
  bool _locked = false;
  DateTime? _leftAt;

  bool get enabled => _enabled;

  /// The screen to cover the app with.
  bool get locked => _enabled && _locked;

  /// Reads the setting; the app starts locked when it is on.
  Future<void> restore() async {
    try {
      final prefs = await SharedPreferences.getInstance();
      _enabled = prefs.getBool(_prefKey) ?? false;
    } on Object {
      _enabled = false;
    }
    _locked = _enabled;
    notifyListeners();
  }

  /// Turns the lock on after proving the phone can do it, or off.
  Future<bool> setEnabled(bool value) async {
    if (value && !await signer.authenticate('Turn on the app lock')) return false;
    _enabled = value;
    _locked = false;
    final prefs = await SharedPreferences.getInstance();
    await prefs.setBool(_prefKey, value);
    notifyListeners();
    return true;
  }

  /// The app left the foreground.
  void left() => _leftAt = _now();

  /// The app is back: locks it when it was away long enough.
  void returned() {
    final left = _leftAt;
    _leftAt = null;
    if (_enabled && left != null && _now().difference(left) >= grace && !_locked) {
      _locked = true;
      notifyListeners();
    }
  }

  /// Asks for the fingerprint or the screen lock.
  Future<void> unlock() async {
    if (!_locked) return;
    try {
      if (await signer.authenticate('Unlock Pouple')) {
        _locked = false;
        notifyListeners();
      }
    } on SignerException {
      // Stays locked; the screen offers to try again.
    }
  }
}
