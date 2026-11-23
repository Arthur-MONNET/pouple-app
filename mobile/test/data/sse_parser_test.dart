import 'dart:async';
import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:pouple_companion/src/data/net/sse_parser.dart';

Stream<List<int>> _chunks(List<String> parts) => Stream.fromIterable(parts.map(utf8.encode));

void main() {
  test('parses named events and skips keep-alive comments', () async {
    final events = await SseParser.parse(
      _chunks([
        'event: snapshot\ndata: {"a":1}\n\n',
        ': keep-alive\n\n',
        'event: snapshot\ndata: {"a":2}\n\n',
      ]),
    ).toList();
    expect(events.map((e) => e.name), ['snapshot', 'snapshot']);
    expect(events.map((e) => e.data), ['{"a":1}', '{"a":2}']);
  });

  test('reassembles an event split across chunks, even in the middle of a character', () async {
    final bytes = utf8.encode('event: snapshot\ndata: {"t":"é"}\n\n');
    final cut = bytes.indexOf(0xC3) + 1;
    final events = await SseParser.parse(Stream.fromIterable([bytes.sublist(0, cut), bytes.sublist(cut)]))
        .toList();
    expect(events.single.data, '{"t":"é"}');
  });

  test('joins several data lines and handles CRLF', () async {
    final events = await SseParser.parse(_chunks(['event: x\r\ndata: one\r\ndata: two\r\n\r\n'])).toList();
    expect(events.single.data, 'one\ntwo');
  });

  test('an unnamed event is a message', () async {
    final events = await SseParser.parse(_chunks(['data: hi\n\n'])).toList();
    expect(events.single.name, 'message');
  });

  test('an event without data is not dispatched, and an unfinished one is dropped', () async {
    final events = await SseParser.parse(_chunks(['event: x\n\n', 'event: y\ndata: lost'])).toList();
    expect(events, isEmpty);
  });

  test('the event name does not leak into the next event', () async {
    final events = await SseParser.parse(_chunks(['event: a\ndata: 1\n\n', 'data: 2\n\n'])).toList();
    expect(events.map((e) => e.name), ['a', 'message']);
  });
}
