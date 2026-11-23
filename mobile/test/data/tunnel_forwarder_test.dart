import 'dart:async';
import 'dart:io';
import 'dart:math';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:pouple_companion/src/data/net/tunnel_forwarder.dart';

import '../support/tunnel_bridge.dart';

Future<void> _until(bool Function() condition) async {
  final deadline = DateTime.now().add(const Duration(seconds: 5));
  while (!condition()) {
    if (DateTime.now().isAfter(deadline)) fail('Condition not reached in time.');
    await Future<void>.delayed(const Duration(milliseconds: 10));
  }
}

Future<Socket> _open(int port) async {
  final socket = await Socket.connect(InternetAddress.loopbackIPv4, port);
  // A write to a connection the forwarder already dropped fails: that is expected here.
  unawaited(socket.done.then((_) {}, onError: (Object _) {}));
  return socket;
}

/// Completes when the socket ends, by closing or by an error.
Future<void> _closedWhen(Socket socket) {
  final done = Completer<void>();
  socket.listen(
    (_) {},
    onDone: () {
      if (!done.isCompleted) done.complete();
    },
    onError: (Object _) {
      if (!done.isCompleted) done.complete();
    },
  );
  return done.future;
}

void main() {
  late EchoServer echo;
  late TunnelBridge bridge;
  late TunnelForwarder forwarder;

  setUp(() async {
    echo = await EchoServer.start();
    bridge = await TunnelBridge.start(echo.port);
    forwarder = await TunnelForwarder.start('unused.ngrok-free.app', upstream: bridge.uri);
  });
  tearDown(() async {
    await forwarder.close();
    await bridge.close();
    await echo.close();
  });

  Future<Socket> connect() => _open(forwarder.port);

  test('listens on the loopback only, on an ephemeral port', () async {
    expect(forwarder.port, greaterThan(0));
    final socket = await connect();
    expect(socket.remoteAddress.address, '127.0.0.1');
    socket.destroy();
  });

  test('carries bytes both ways and sends the ngrok header on the upgrade', () async {
    final socket = await connect();
    final received = <int>[];
    final done = socket.listen(received.addAll).asFuture<void>();
    socket.add([1, 2, 3, 4]);
    await _until(() => received.length == 4);
    expect(received, [1, 2, 3, 4]);
    expect(bridge.skipHeaders, ['1']);
    socket.destroy();
    await done.catchError((_) {});
  });

  test('moves a large payload both ways with back-pressure, intact', () async {
    final random = Random(7);
    final payload = Uint8List.fromList(List.generate(6 * 1024 * 1024, (_) => random.nextInt(256)));
    final socket = await connect();
    final back = BytesBuilder(copy: false);
    final finished = Completer<void>();
    socket.listen((chunk) {
      back.add(chunk);
      if (back.length == payload.length && !finished.isCompleted) finished.complete();
    });
    await socket.addStream(Stream.value(payload));
    await finished.future.timeout(const Duration(seconds: 20));
    expect(back.takeBytes(), payload);
    socket.destroy();
  });

  test('carries many connections at once without mixing them', () async {
    const count = 60;
    final results = await Future.wait([
      for (var i = 0; i < count; i++)
        () async {
          final socket = await connect();
          final expected = Uint8List.fromList(List.generate(2000, (j) => (i + j) % 256));
          final back = BytesBuilder(copy: false);
          final done = Completer<void>();
          socket.listen((chunk) {
            back.add(chunk);
            if (back.length == expected.length && !done.isCompleted) done.complete();
          });
          socket.add(expected);
          await done.future.timeout(const Duration(seconds: 10));
          final same = back.takeBytes().toList().toString() == expected.toList().toString();
          socket.destroy();
          return same;
        }(),
    ]);
    expect(results, everyElement(isTrue));
    expect(echo.connections, count);
  });

  test('a phone-side close closes the WebSocket and releases the connection', () async {
    final socket = await connect();
    socket.add([1]);
    await _until(() => bridge.opened == 1 && forwarder.activeConnections == 1);
    socket.destroy();
    await _until(() => bridge.closed == 1);
    await _until(() => forwarder.activeConnections == 0);
  });

  test('a tunnel-side close closes the phone-side socket', () async {
    final socket = await connect();
    final done = _closedWhen(socket);
    socket.add([1]);
    await _until(() => bridge.opened == 1);
    await bridge.close();
    await done.timeout(const Duration(seconds: 5));
    await _until(() => forwarder.activeConnections == 0);
  });

  test('closing the forwarder drops every connection and frees the port', () async {
    final sockets = [for (var i = 0; i < 5; i++) await connect()];
    final dones = [
      for (final socket in sockets)
        () {
          final done = _closedWhen(socket);
          socket.add([1]);
          return done;
        }(),
    ];
    await _until(() => bridge.opened == 5);
    final port = forwarder.port;
    await forwarder.close();
    await Future.wait(dones).timeout(const Duration(seconds: 5));
    await _until(() => bridge.closed == 5);
    expect(forwarder.activeConnections, 0);
    await expectLater(Socket.connect(InternetAddress.loopbackIPv4, port), throwsA(isA<SocketException>()));
    await forwarder.close(); // twice is fine
  });

  test('an unreachable tunnel closes the socket and is counted', () async {
    await bridge.close();
    final socket = await connect();
    final done = _closedWhen(socket);
    socket.add([1]);
    await done.timeout(const Duration(seconds: 5));
    expect(forwarder.upstreamFailures, 1);
    expect(forwarder.activeConnections, 0);
  });

  test('a tunnel that answers something else than a WebSocket upgrade counts as down', () async {
    final plain = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    plain.listen((request) {
      request.response.statusCode = 404;
      request.response.close();
    });
    addTearDown(() => plain.close(force: true));
    final other = await TunnelForwarder.start(
      'x.example',
      upstream: Uri.parse('ws://127.0.0.1:${plain.port}/tunnel/v1'),
    );
    addTearDown(other.close);
    final socket = await _open(other.port);
    final done = _closedWhen(socket);
    socket.add([1]);
    await done.timeout(const Duration(seconds: 5));
    expect(other.upstreamFailures, 1);
  });
}
