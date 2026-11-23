import 'package:flutter/services.dart';

/// The fingerprint step-up key could not be used.
enum SignerProblem {
  /// The person closed the fingerprint prompt.
  cancelled,

  /// No fingerprint is enrolled, or the device cannot do it.
  unavailable,

  /// The key was invalidated (a new fingerprint was enrolled): pair again.
  invalidated,

  /// Anything else.
  failed,
}

class SignerException implements Exception {
  const SignerException(this.problem);

  final SignerProblem problem;

  String get display => switch (problem) {
    SignerProblem.cancelled => 'Approval cancelled.',
    SignerProblem.unavailable => 'Fingerprint approval is not available on this phone.',
    SignerProblem.invalidated => 'The fingerprint key is no longer valid. Unpair and pair again.',
    SignerProblem.failed => 'The fingerprint approval failed.',
  };

  @override
  String toString() => 'SignerException(${problem.name})';
}

/// The phone's step-up key and its biometric prompt. Implemented in Kotlin on Android (Keystore P-256, per-use
/// authentication); faked in tests.
abstract interface class StepUpSigner {
  /// Creates the P-256 key (replacing any earlier one) and returns its X.509 public key, standard base64 with padding.
  Future<String> createKey();

  /// Signs [message] (UTF-8) with SHA256withECDSA after a fingerprint prompt. Returns the DER signature, base64.
  Future<String> sign(String message, {required String title, required String subtitle});

  /// Removes the key.
  Future<void> deleteKey();

  /// Asks for a fingerprint or the screen lock, without a key (the optional app lock).
  Future<bool> authenticate(String title);

  /// A name for this phone, for the Mac's list of devices.
  Future<String> deviceName();
}

/// The Android implementation, over a platform channel.
class PlatformStepUpSigner implements StepUpSigner {
  const PlatformStepUpSigner();

  static const _channel = MethodChannel('app.pouple.companion/security');

  Future<T> _call<T>(String method, [Map<String, Object?>? arguments]) async {
    try {
      final result = await _channel.invokeMethod<T>(method, arguments);
      if (result == null) throw const SignerException(SignerProblem.failed);
      return result;
    } on PlatformException catch (error) {
      throw SignerException(switch (error.code) {
        'cancelled' => SignerProblem.cancelled,
        'unavailable' => SignerProblem.unavailable,
        'invalidated' => SignerProblem.invalidated,
        _ => SignerProblem.failed,
      });
    } on MissingPluginException {
      throw const SignerException(SignerProblem.unavailable);
    }
  }

  @override
  Future<String> createKey() => _call<String>('createKey');

  @override
  Future<String> sign(String message, {required String title, required String subtitle}) =>
      _call<String>('sign', {'message': message, 'title': title, 'subtitle': subtitle});

  @override
  Future<void> deleteKey() async {
    try {
      await _channel.invokeMethod<void>('deleteKey');
    } on PlatformException {
      // Nothing to remove, or already gone.
    } on MissingPluginException {
      // Not on a device.
    }
  }

  @override
  Future<bool> authenticate(String title) async {
    try {
      return await _call<bool>('authenticate', {'title': title});
    } on SignerException catch (error) {
      if (error.problem == SignerProblem.cancelled) return false;
      rethrow;
    }
  }

  @override
  Future<String> deviceName() async {
    try {
      return await _call<String>('deviceName');
    } on SignerException {
      return 'Android phone';
    }
  }
}
