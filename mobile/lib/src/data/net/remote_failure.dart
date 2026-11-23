/// Why a call to the Mac failed.
enum FailureKind {
  /// No address answered (Mac asleep, other network, access turned off).
  unreachable,

  /// The certificate is not the pinned one: the Mac made a new one, or this is not the Mac.
  certificateMismatch,

  /// The token is unknown, revoked or expired: pair again.
  unauthorized,

  /// The request needs a fingerprint signature it did not carry.
  stepUpRequired,

  /// The fingerprint signature was refused.
  stepUpRefused,

  /// The pairing window is closed or the code is wrong.
  pairingRefused,

  /// 404.
  notFound,

  /// 409: the conversation changed; read it again.
  conflict,

  /// Any other 4xx (a rule of the Mac's, shown with its own message).
  rejected,

  /// 5xx, or an answer this app cannot read.
  server,
}

/// A failed call. [message] is the text the Mac wrote or a fixed text of this app, never a conversation's content.
class RemoteFailure implements Exception {
  const RemoteFailure(this.kind, {this.status, this.code, this.message});

  final FailureKind kind;
  final int? status;

  /// The remote layer's error code (`pairing-closed`, `too-many-devices`...), when there is one.
  final String? code;
  final String? message;

  /// What to show the person.
  String get display {
    final given = message;
    if (given != null && given.isNotEmpty) return given;
    switch (kind) {
      case FailureKind.unreachable:
        return 'The Mac cannot be reached.';
      case FailureKind.certificateMismatch:
        return 'This is not the Mac that was paired (its certificate changed). Pair again.';
      case FailureKind.unauthorized:
        return 'This phone is no longer paired. Pair it again.';
      case FailureKind.stepUpRequired:
      case FailureKind.stepUpRefused:
        return 'The fingerprint approval was not accepted.';
      case FailureKind.pairingRefused:
        return 'Pairing was refused. Open a new pairing window on the Mac.';
      case FailureKind.notFound:
        return 'This conversation no longer exists.';
      case FailureKind.conflict:
        return 'The conversation changed. Check it and try again.';
      case FailureKind.rejected:
        return 'The Mac refused this request.';
      case FailureKind.server:
        return 'The Mac sent an unexpected answer.';
    }
  }

  @override
  String toString() => 'RemoteFailure(${kind.name}, status: $status, code: $code)';
}
