import 'dart:async';
import 'dart:io';

/// A loopback TCP listener that carries each connection, byte for byte, inside one binary WebSocket to
/// `wss://<domain>/tunnel/v1`.
///
/// It adds no cryptography: what travels through is the TLS session between the pinned HTTP client and the Mac, so
/// the tunnel (ngrok) only ever sees ciphertext. The outer `wss` connection is validated with the system roots.
/// Nothing is logged.
class TunnelForwarder {
  TunnelForwarder._(this._server, this._uri, this._client, this._connectTimeout, this._pingInterval) {
    _server.listen(_accept, onError: (_) {}, cancelOnError: false);
  }

  static const path = '/tunnel/v1';

  /// Binds `127.0.0.1` on an ephemeral port. [upstream] replaces `wss://<domain>/tunnel/v1` (tests only).
  static Future<TunnelForwarder> start(
    String domain, {
    Uri? upstream,
    Duration connectTimeout = const Duration(seconds: 10),
    Duration pingInterval = const Duration(seconds: 20),
  }) async {
    final server = await ServerSocket.bind(InternetAddress.loopbackIPv4, 0);
    final client = HttpClient()..connectionTimeout = connectTimeout;
    return TunnelForwarder._(
      server,
      upstream ?? Uri(scheme: 'wss', host: domain, path: path),
      client,
      connectTimeout,
      pingInterval,
    );
  }

  final ServerSocket _server;
  final Uri _uri;
  final HttpClient _client;
  final Duration _connectTimeout;
  final Duration _pingInterval;
  final Set<_Link> _links = {};
  bool _closed = false;
  int _upstreamFailures = 0;

  /// The loopback port the HTTP client connects to.
  int get port => _server.port;

  /// How many times the WebSocket to the tunnel could not be opened. A client whose TLS handshake dies reads this
  /// to tell "the tunnel is down" from "the certificate was refused".
  int get upstreamFailures => _upstreamFailures;

  /// Connections currently carried.
  int get activeConnections => _links.length;

  void _accept(Socket socket) {
    if (_closed) {
      socket.destroy();
      return;
    }
    final link = _Link(socket);
    _links.add(link);
    link.onClose(() => _links.remove(link));
    unawaited(_run(link));
  }

  Future<void> _run(_Link link) async {
    try {
      link.socket.setOption(SocketOption.tcpNoDelay, true);
      // The socket is not read until it is listened to, so the phone's first bytes (the TLS ClientHello) wait in
      // the operating system while the WebSocket opens.
      final WebSocket ws;
      try {
        ws = await _connect();
      } on Object {
        _upstreamFailures++;
        link.close();
        return;
      }
      if (link.closed) {
        unawaited(ws.close().timeout(const Duration(seconds: 2)).then((_) {}, onError: (_) {}));
        return;
      }
      // Phone -> tunnel. addStream pauses the socket while the WebSocket cannot take more. The socket goes through
      // a controller so that closing the link can end the stream, which lets the WebSocket send its close frame.
      final up = StreamController<List<int>>(sync: true);
      final subscription = link.socket.listen(up.add, onError: (_) => up.close(), onDone: up.close);
      up
        ..onPause = subscription.pause
        ..onResume = subscription.resume;
      link.onClose(() {
        unawaited(subscription.cancel());
        // End the outgoing stream first: the close frame can only follow once it is done.
        unawaited(
          up.close().then((_) => ws.close(WebSocketStatus.normalClosure)).then((_) {}, onError: (_) {}),
        );
      });
      ws.pingInterval = _pingInterval;
      unawaited(ws.addStream(up.stream).then((_) {}, onError: (_) {}).whenComplete(link.close));
      // Tunnel -> phone. Socket.addStream pauses the WebSocket while the socket cannot take more. What was
      // received is flushed before the socket is dropped.
      final down = link.socket.addStream(
        ws.map<List<int>>((message) {
          if (message is List<int>) return message;
          throw const FormatException('Text message in a binary tunnel.');
        }),
      );
      unawaited(down.then((_) => link.socket.flush()).then((_) {}, onError: (_) {}).whenComplete(link.close));
    } on Object {
      link.close();
    }
  }

  Future<WebSocket> _connect() async {
    final future = WebSocket.connect(
      _uri.toString(),
      headers: const {'ngrok-skip-browser-warning': '1'},
      customClient: _client,
    );
    try {
      return await future.timeout(_connectTimeout);
    } on TimeoutException {
      // The attempt may still finish later: do not leave that socket open.
      unawaited(future.then((late) => late.close(), onError: (_) {}));
      rethrow;
    }
  }

  /// Stops listening and drops every connection. Safe to call twice.
  Future<void> close() async {
    if (_closed) return;
    _closed = true;
    for (final link in _links.toList()) {
      link.close();
    }
    _client.close(force: true);
    await _server.close();
  }
}

/// One accepted connection and what must be released when either end goes.
class _Link {
  _Link(this.socket);

  final Socket socket;
  final List<void Function()> _cleanups = [];
  bool closed = false;

  void onClose(void Function() cleanup) => _cleanups.add(cleanup);

  void close() {
    if (closed) return;
    closed = true;
    for (final cleanup in _cleanups) {
      try {
        cleanup();
      } on Object {
        // Releasing is best effort.
      }
    }
    socket.destroy();
  }
}
