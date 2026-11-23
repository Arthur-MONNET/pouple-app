import 'dart:async';
import 'dart:io';

/// Stands in for ngrok plus the Mac's tunnel handler: a plain WebSocket server on `/tunnel/v1` that pipes every
/// connection, as raw bytes, to a local TCP port (a TLS server, or an echo server).
class TunnelBridge {
  TunnelBridge._(this._server, this.targetPort);

  final HttpServer _server;
  final int targetPort;

  /// The `ngrok-skip-browser-warning` header of every upgrade.
  final List<String?> skipHeaders = [];
  int opened = 0;
  int closed = 0;
  final List<Socket> _targets = [];

  static Future<TunnelBridge> start(int targetPort) async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final bridge = TunnelBridge._(server, targetPort);
    server.listen(bridge._handle);
    return bridge;
  }

  int get port => _server.port;

  Uri get uri => Uri.parse('ws://127.0.0.1:$port/tunnel/v1');

  Future<void> _handle(HttpRequest request) async {
    if (request.uri.path != '/tunnel/v1' || !WebSocketTransformer.isUpgradeRequest(request)) {
      request.response.statusCode = 404;
      await request.response.close();
      return;
    }
    skipHeaders.add(request.headers.value('ngrok-skip-browser-warning'));
    final ws = await WebSocketTransformer.upgrade(request);
    final Socket target;
    try {
      target = await Socket.connect(InternetAddress.loopbackIPv4, targetPort);
    } on SocketException {
      await ws.close();
      return;
    }
    _targets.add(target);
    opened++;
    var done = false;
    void finish() {
      if (done) return;
      done = true;
      closed++;
      target.destroy();
      ws.close();
    }

    ws.listen((message) => target.add(message as List<int>), onDone: finish, onError: (_) => finish());
    target.listen(ws.add, onDone: finish, onError: (_) => finish());
  }

  Future<void> close() async {
    for (final target in _targets) {
      target.destroy();
    }
    await _server.close(force: true);
  }
}

/// A TCP server that sends back what it receives.
class EchoServer {
  EchoServer._(this._server);

  final ServerSocket _server;
  int connections = 0;

  static Future<EchoServer> start() async {
    final server = await ServerSocket.bind(InternetAddress.loopbackIPv4, 0);
    final echo = EchoServer._(server);
    server.listen((socket) {
      echo.connections++;
      socket.listen(socket.add, onDone: socket.destroy, onError: (_) => socket.destroy());
    });
    return echo;
  }

  int get port => _server.port;

  Future<void> close() => _server.close();
}
