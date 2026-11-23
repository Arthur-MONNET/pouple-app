import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:pouple_companion/src/data/net/remote_client.dart';
import 'package:pouple_companion/src/data/stepup/step_up_signer.dart';

/// One request the fake Mac received.
class Seen {
  Seen(this.method, this.path, this.headers, this.body);

  final String method;
  final String path;
  final HttpHeaders headers;
  final List<int> body;

  String get bodyText => utf8.decode(body);
}

/// A plain-HTTP loopback server standing in for the Mac (TLS pinning is covered by its own tests).
class FakeMac {
  FakeMac._(this._server) : port = _server.port;

  final HttpServer _server;
  final List<Seen> seen = [];

  /// Answers by "METHOD path"; the default is 204.
  final Map<String, (int, Object?)> answers = {};

  /// Required header values (a step-up policy): "METHOD path" must carry a nonce, or this answer is given.
  final Set<String> stepUpRoutes = {};

  static Future<FakeMac> start() async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    final mac = FakeMac._(server);
    server.listen(mac._handle);
    return mac;
  }

  /// Kept after the server closes, so a client can still aim at the dead address.
  final int port;

  Endpoint get endpoint => Endpoint(hosts: const ['127.0.0.1'], port: port, fingerprint: 'unused');

  RemoteClient client({String? token = 'tok'}) =>
      RemoteClient(endpoint: endpoint, token: token, scheme: 'http');

  Future<void> close() => _server.close(force: true);

  Future<void> _handle(HttpRequest request) async {
    final body = await request.fold<List<int>>([], (all, chunk) => all..addAll(chunk));
    final key = '${request.method} ${request.uri.path}';
    seen.add(Seen(request.method, request.uri.path, request.headers, body));
    final answer = answers[key];
    if (answer == null && request.uri.path == '/remote/v1/challenge') {
      request.response
        ..statusCode = 200
        ..write(jsonEncode({'nonce': 'nonce0001', 'expiresAt': 'x'}));
    } else if (stepUpRoutes.contains(key) && request.headers.value('X-Pouple-Nonce') == null) {
      request.response
        ..statusCode = 403
        ..write(jsonEncode({'error': 'step-up-required'}));
    } else {
      final (status, payload) = answer ?? (204, null);
      request.response.statusCode = status;
      if (payload != null) request.response.write(payload is String ? payload : jsonEncode(payload));
    }
    await request.response.close();
  }
}

/// A signer that records what it was asked to sign.
class FakeSigner implements StepUpSigner {
  final List<String> signed = [];
  final List<String> titles = [];
  SignerException? failure;
  bool authenticated = true;
  bool keyCreated = false;
  bool keyDeleted = false;

  @override
  Future<String> createKey() async {
    keyCreated = true;
    return 'cHVibGljLWtleQ==';
  }

  @override
  Future<String> sign(String message, {required String title, required String subtitle}) async {
    final error = failure;
    if (error != null) throw error;
    signed.add(message);
    titles.add(title);
    return 'c2lnbmF0dXJl';
  }

  @override
  Future<void> deleteKey() async => keyDeleted = true;

  @override
  Future<bool> authenticate(String title) async => authenticated;

  @override
  Future<String> deviceName() async => 'Test phone';
}
