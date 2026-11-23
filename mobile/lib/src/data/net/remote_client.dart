import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import '../models/assistant.dart';
import '../models/json_reader.dart';
import 'cert_pinning.dart';
import 'remote_failure.dart';
import 'sse_parser.dart';

/// Where the Mac listens and the certificate to pin.
class Endpoint {
  const Endpoint({required this.hosts, required this.port, required this.fingerprint});

  /// Addresses to try in order. An IPv6 address has no brackets here.
  final List<String> hosts;
  final int port;
  final String fingerprint;
}

/// A fingerprint step-up that goes with one request.
class StepUpHeaders {
  const StepUpHeaders({required this.nonce, required this.signature});

  final String nonce;

  /// Standard base64 of the DER ECDSA signature.
  final String signature;
}

/// An answer of the Mac.
class RemoteResponse {
  const RemoteResponse(this.status, this.body);

  final int status;
  final Uint8List body;

  Object? get json => body.isEmpty ? null : jsonDecode(utf8.decode(body));
}

/// The Mac's phone API over TLS with a pinned certificate.
///
/// Never logs a token, a body or an answer.
class RemoteClient {
  RemoteClient({
    required this.endpoint,
    this.token,
    HttpClient Function(String fingerprint)? clientFactory,
    this.scheme = 'https',
    this.requestTimeout = const Duration(seconds: 20),
  }) : _factory = clientFactory ?? createPinnedHttpClient;

  final Endpoint endpoint;
  final String? token;

  /// Always `https`; tests with a plain loopback server use `http`.
  final String scheme;
  final Duration requestTimeout;
  final HttpClient Function(String fingerprint) _factory;

  HttpClient? _client;
  String? _lastGoodHost;

  HttpClient get _http => _client ??= _factory(endpoint.fingerprint);

  /// Closes open connections, streams included.
  void close() {
    _client?.close(force: true);
    _client = null;
  }

  List<String> get _hostsInOrder {
    final good = _lastGoodHost;
    if (good == null || !endpoint.hosts.contains(good)) return endpoint.hosts;
    return [good, ...endpoint.hosts.where((host) => host != good)];
  }

  Uri _uri(String host, String path, Map<String, String>? query) =>
      Uri(scheme: scheme, host: host, port: endpoint.port, path: path, queryParameters: query);

  /// Sends one request, trying each address until one answers.
  Future<RemoteResponse> request(
    String method,
    String path, {
    Uint8List? body,
    Map<String, String>? query,
    StepUpHeaders? stepUp,
    bool authenticated = true,
  }) async {
    Object? lastError;
    for (final host in _hostsInOrder) {
      try {
        final request = await _http.openUrl(method, _uri(host, path, query)).timeout(requestTimeout);
        _decorate(request, authenticated: authenticated, stepUp: stepUp, hasBody: body != null);
        if (body != null) request.add(body);
        final response = await request.close().timeout(requestTimeout);
        _checkCertificate(response);
        final bytes = await _readAll(response).timeout(requestTimeout);
        _lastGoodHost = host;
        return _checked(RemoteResponse(response.statusCode, bytes));
      } on RemoteFailure {
        rethrow;
      } on HandshakeException {
        lastError = const RemoteFailure(FailureKind.certificateMismatch);
      } on TlsException {
        lastError = const RemoteFailure(FailureKind.certificateMismatch);
      } on SocketException {
        lastError = const RemoteFailure(FailureKind.unreachable);
      } on TimeoutException {
        lastError = const RemoteFailure(FailureKind.unreachable);
      } on HttpException {
        lastError = const RemoteFailure(FailureKind.unreachable);
      }
    }
    // A pinned-certificate refusal on the last address is the more useful reason to give.
    throw lastError is RemoteFailure ? lastError : const RemoteFailure(FailureKind.unreachable);
  }

  void _decorate(
    HttpClientRequest request, {
    required bool authenticated,
    required StepUpHeaders? stepUp,
    required bool hasBody,
  }) {
    final key = token;
    if (authenticated && key != null) {
      request.headers.set(HttpHeaders.authorizationHeader, 'Bearer $key');
    }
    if (hasBody) request.headers.contentType = ContentType.json;
    request.headers.set(HttpHeaders.acceptHeader, 'application/json');
    if (stepUp != null) {
      request.headers.set('X-Pouple-Nonce', stepUp.nonce);
      request.headers.set('X-Pouple-Signature', stepUp.signature);
    }
  }

  /// Defense in depth: the handshake already refused any other certificate.
  void _checkCertificate(HttpClientResponse response) {
    if (scheme != 'https') return;
    final certificate = response.certificate;
    if (certificate == null || !fingerprintMatches(certificate.der, endpoint.fingerprint)) {
      throw const RemoteFailure(FailureKind.certificateMismatch);
    }
  }

  Future<Uint8List> _readAll(HttpClientResponse response) async {
    const limit = 64 * 1024 * 1024;
    final builder = BytesBuilder(copy: false);
    await for (final chunk in response) {
      builder.add(chunk);
      if (builder.length > limit) throw const RemoteFailure(FailureKind.server);
    }
    return builder.takeBytes();
  }

  RemoteResponse _checked(RemoteResponse response) {
    final status = response.status;
    if (status >= 200 && status < 300) return response;
    throw failureOf(status, response.body);
  }

  /// Turns a refusal into a [RemoteFailure]: the remote layer answers `{"error":"<code>"}`, the assistant routes
  /// `{"error":"<message>"}`.
  static RemoteFailure failureOf(int status, Uint8List body) {
    String? error;
    try {
      final decoded = body.isEmpty ? null : jsonDecode(utf8.decode(body));
      if (decoded is Map && decoded['error'] is String) error = decoded['error'] as String;
    } on FormatException {
      error = null;
    }
    switch (error) {
      case 'unauthorized':
        return RemoteFailure(FailureKind.unauthorized, status: status, code: error);
      case 'step-up-required':
        return RemoteFailure(FailureKind.stepUpRequired, status: status, code: error);
      case 'step-up-refused':
        return RemoteFailure(FailureKind.stepUpRefused, status: status, code: error);
      case 'pairing-closed':
        return RemoteFailure(
          FailureKind.pairingRefused,
          status: status,
          code: error,
          message: 'The pairing window is closed. Open a new one on the Mac.',
        );
      case 'pairing-refused':
        return RemoteFailure(
          FailureKind.pairingRefused,
          status: status,
          code: error,
          message: 'The pairing code was refused. Open a new pairing window on the Mac.',
        );
      case 'too-many-devices':
        return RemoteFailure(
          FailureKind.rejected,
          status: status,
          code: error,
          message: 'Ten phones are already paired. Remove one on the Mac first.',
        );
      case 'forbidden-origin':
      case 'forbidden-route':
      case 'bad-request':
      case 'bad-device-name':
      case 'bad-key':
      case 'body-too-large':
        return RemoteFailure(FailureKind.rejected, status: status, code: error);
    }
    if (status == 401) return RemoteFailure(FailureKind.unauthorized, status: status);
    if (status == 404) return RemoteFailure(FailureKind.notFound, status: status, message: error);
    if (status == 409) return RemoteFailure(FailureKind.conflict, status: status, message: error);
    if (status >= 500) return RemoteFailure(FailureKind.server, status: status);
    return RemoteFailure(FailureKind.rejected, status: status, message: error);
  }

  /// Opens the snapshot stream. Ends when the connection closes; fails with a [RemoteFailure] when it cannot open.
  /// Each event is a full snapshot.
  Stream<AssistantSnapshot> snapshots() async* {
    Object? lastError;
    for (final host in _hostsInOrder) {
      HttpClientResponse response;
      try {
        final request = await _http
            .openUrl('GET', _uri(host, '/api/assistant/stream', null))
            .timeout(requestTimeout);
        _decorate(request, authenticated: true, stepUp: null, hasBody: false);
        request.headers.set(HttpHeaders.acceptHeader, 'text/event-stream');
        response = await request.close().timeout(requestTimeout);
        _checkCertificate(response);
      } on RemoteFailure {
        rethrow;
      } on TlsException {
        lastError = const RemoteFailure(FailureKind.certificateMismatch);
        continue;
      } on SocketException {
        lastError = const RemoteFailure(FailureKind.unreachable);
        continue;
      } on TimeoutException {
        lastError = const RemoteFailure(FailureKind.unreachable);
        continue;
      } on HttpException {
        lastError = const RemoteFailure(FailureKind.unreachable);
        continue;
      }
      _lastGoodHost = host;
      if (response.statusCode != 200) {
        final bytes = await _readAll(response);
        throw failureOf(response.statusCode, bytes);
      }
      // The server sends a keep-alive every 15 seconds: silence for a minute means the link is dead.
      final bytes = response.timeout(
        const Duration(seconds: 60),
        onTimeout: (sink) => sink
          ..addError(const RemoteFailure(FailureKind.unreachable))
          ..close(),
      );
      try {
        await for (final event in SseParser.parse(bytes)) {
          if (event.name != 'snapshot') continue;
          yield AssistantSnapshot.fromJson(jsonDecode(event.data));
        }
      } on SocketException {
        throw const RemoteFailure(FailureKind.unreachable);
      } on HttpException {
        throw const RemoteFailure(FailureKind.unreachable);
      } on FormatException {
        throw const RemoteFailure(FailureKind.server);
      } on SseOverflow {
        throw const RemoteFailure(FailureKind.server);
      }
      return;
    }
    throw lastError is RemoteFailure ? lastError : const RemoteFailure(FailureKind.unreachable);
  }

  // Remote layer -----------------------------------------------------------------------------------------------

  /// POST /remote/v1/pair. Returns the device id and token.
  Future<({String deviceId, String token})> pair({
    required String code,
    required String deviceName,
    required String stepUpPublicKey,
  }) async {
    final body = utf8.encode(
      jsonEncode({'code': code, 'deviceName': deviceName, 'stepUpPublicKey': stepUpPublicKey}),
    );
    final response = await request(
      'POST',
      '/remote/v1/pair',
      body: Uint8List.fromList(body),
      authenticated: false,
    );
    try {
      final json = JsonObject.of(response.json);
      return (deviceId: json.string('deviceId'), token: json.string('token'));
    } on FormatException {
      throw const RemoteFailure(FailureKind.server);
    }
  }

  /// GET /remote/v1/device: checks that the pairing still works.
  Future<void> checkDevice() async {
    await request('GET', '/remote/v1/device');
  }

  /// DELETE /remote/v1/device: unpairs this phone.
  Future<void> unpairSelf() async {
    await request('DELETE', '/remote/v1/device');
  }

  /// POST /remote/v1/challenge: a nonce for a step-up, valid for 60 seconds.
  Future<String> challenge() async {
    final response = await request('POST', '/remote/v1/challenge');
    try {
      return JsonObject.of(response.json).string('nonce');
    } on FormatException {
      throw const RemoteFailure(FailureKind.server);
    }
  }

  // Assistant routes ----------------------------------------------------------------------------------------------

  /// GET /api/assistant.
  Future<AssistantSnapshot> snapshot() async {
    final response = await request('GET', '/api/assistant');
    try {
      return AssistantSnapshot.fromJson(response.json);
    } on FormatException {
      throw const RemoteFailure(FailureKind.server);
    }
  }

  /// GET /api/assistant/projects.
  Future<List<ProjectInfo>> projects() async {
    final response = await request('GET', '/api/assistant/projects');
    try {
      return JsonObject.of(response.json).objects('projects').map(ProjectInfo.fromJson).toList();
    } on FormatException {
      throw const RemoteFailure(FailureKind.server);
    }
  }
}
