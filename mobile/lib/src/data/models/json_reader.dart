/// Strict reading of the Mac's JSON.
///
/// A required field of the wrong type is a [FormatException] that names the field path only, never its value, so a
/// malformed answer cannot leak conversation content into an error message or a log.
library;

/// A JSON object read with typed accessors.
class JsonObject {
  JsonObject._(this._map, this.path);

  /// Reads [value] as an object, or throws.
  factory JsonObject.of(Object? value, [String path = r'$']) {
    if (value is Map<String, dynamic>) return JsonObject._(value, path);
    if (value is Map) {
      return JsonObject._(value.map((key, value) => MapEntry('$key', value)), path);
    }
    throw FormatException('Expected an object at $path');
  }

  final Map<String, dynamic> _map;

  /// Where this object sits in the document, for error messages.
  final String path;

  FormatException _bad(String key, String expected) => FormatException('Expected $expected at $path.$key');

  /// Whether [key] is present with a non-null value.
  bool has(String key) => _map[key] != null;

  /// A required string.
  String string(String key) {
    final value = _map[key];
    if (value is String) return value;
    throw _bad(key, 'a string');
  }

  /// A string that may be absent or null.
  String? optString(String key) {
    final value = _map[key];
    if (value == null) return null;
    if (value is String) return value;
    throw _bad(key, 'a string or null');
  }

  /// A string that defaults to [fallback] when absent or null.
  String stringOr(String key, String fallback) => optString(key) ?? fallback;

  /// A boolean that defaults to [fallback] when absent or null.
  bool boolOr(String key, bool fallback) {
    final value = _map[key];
    if (value == null) return fallback;
    if (value is bool) return value;
    throw _bad(key, 'a boolean');
  }

  /// A required integer.
  int integer(String key) {
    final value = _map[key];
    if (value is int) return value;
    throw _bad(key, 'an integer');
  }

  /// An integer that may be absent or null.
  int? optInteger(String key) {
    final value = _map[key];
    if (value == null) return null;
    if (value is int) return value;
    throw _bad(key, 'an integer or null');
  }

  /// A required nested object.
  JsonObject object(String key) => JsonObject.of(_map[key], '$path.$key');

  /// A nested object that may be absent or null.
  JsonObject? optObject(String key) {
    final value = _map[key];
    if (value == null) return null;
    return JsonObject.of(value, '$path.$key');
  }

  /// A list of objects; absent or null is the empty list (Go encodes a nil slice as null).
  List<JsonObject> objects(String key) {
    final value = _map[key];
    if (value == null) return const [];
    if (value is! List) throw _bad(key, 'a list');
    return [for (var i = 0; i < value.length; i++) JsonObject.of(value[i], '$path.$key[$i]')];
  }

  /// A list of strings; absent or null is the empty list.
  List<String> strings(String key) {
    final value = _map[key];
    if (value == null) return const [];
    if (value is! List) throw _bad(key, 'a list');
    return [
      for (final item in value)
        if (item is String) item else throw _bad(key, 'a list of strings'),
    ];
  }

  /// The length of a list that may be absent or null (used where only its size matters).
  int listLength(String key) {
    final value = _map[key];
    if (value == null) return 0;
    if (value is List) return value.length;
    throw _bad(key, 'a list');
  }
}
