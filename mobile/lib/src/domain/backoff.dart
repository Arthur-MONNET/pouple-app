/// Delays between reconnection attempts: 1 s, doubling to a ceiling, back to the start after a success.
class Backoff {
  Backoff({this.initial = const Duration(seconds: 1), this.ceiling = const Duration(seconds: 30)});

  final Duration initial;
  final Duration ceiling;
  int _failures = 0;

  /// The delay before the next attempt; each call lengthens the following one.
  Duration next() {
    var delay = initial;
    for (var i = 0; i < _failures && delay < ceiling; i++) {
      delay *= 2;
    }
    _failures++;
    return delay > ceiling ? ceiling : delay;
  }

  /// A connection worked: start again from the initial delay.
  void reset() => _failures = 0;
}
