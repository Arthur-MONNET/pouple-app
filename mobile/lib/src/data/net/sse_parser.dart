import 'dart:async';
import 'dart:convert';

/// One server-sent event.
class SseEvent {
  const SseEvent({required this.name, required this.data});

  /// The `event:` field; "message" when the server did not name it.
  final String name;

  /// The `data:` lines joined with newlines.
  final String data;
}

/// The stream carries more than [SseParser.maxEventBytes] in one event: the connection is dropped.
class SseOverflow implements Exception {
  const SseOverflow();
}

/// Turns the bytes of a `text/event-stream` into events. Comments (`: keep-alive`) are skipped.
class SseParser {
  /// An event larger than this is refused (a snapshot of many long conversations stays far below).
  static const maxEventBytes = 32 * 1024 * 1024;

  /// Parses [bytes] as they arrive.
  static Stream<SseEvent> parse(Stream<List<int>> bytes) async* {
    var name = '';
    final data = <String>[];
    var size = 0;
    await for (final line in utf8.decoder.bind(bytes).transform(const LineSplitter())) {
      if (line.isEmpty) {
        if (data.isNotEmpty) {
          yield SseEvent(name: name.isEmpty ? 'message' : name, data: data.join('\n'));
        }
        name = '';
        data.clear();
        size = 0;
        continue;
      }
      if (line.startsWith(':')) continue;
      final colon = line.indexOf(':');
      final field = colon < 0 ? line : line.substring(0, colon);
      var value = colon < 0 ? '' : line.substring(colon + 1);
      if (value.startsWith(' ')) value = value.substring(1);
      switch (field) {
        case 'event':
          name = value;
        case 'data':
          size += value.length + 1;
          if (size > maxEventBytes) throw const SseOverflow();
          data.add(value);
      }
    }
  }
}
