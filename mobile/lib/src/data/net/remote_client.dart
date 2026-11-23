import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter/foundation.dart';

import '../models/assistant.dart';
import '../models/json_reader.dart';
import 'cert_pinning.dart';
import 'external_address.dart';
import 'remote_failure.dart';
import 'route_memory.dart';
import 'sse_parser.dart';

export 'external_address.dart';
export 'route_memory.dart';

/// Where the Mac listens and the certificate to pin.
class Endpoint {
  const Endpoint({required this.hosts, required this.port, required this.fingerprint, this.external});

  /// Home network addresses to try in order. An IPv6 address has no brackets here.
  final List<String> hosts;
  final int port;
  final String fingerprint;

  /// Where the router exposes the Mac to the Internet, if it does.
  final ExternalAddress? external;

  Endpoint withExternal(ExternalAddress? address) =>
      Endpoint(hosts: hosts, port: port, fingerprint: fingerprint, external: address);
}

/// One address to try, and which way it goes. The pinned fingerprint is the same for every candidate.
class Candidate {
  const Candidate(this.host, this.port, this.route);

  final String host;
  final int port;
  final NetworkRoute route;
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
/// Tries the home network addresses first (with a short timeout), then the external address, and remembers which
/// route worked. A certificate that is not the pinned one is a hard failure on any of them: it never falls back to
/// another address. Never logs a token, a body, an answer or an address.
class RemoteClient {
  RemoteClient({
    required this._endpoint,
    this.token,
    HttpClient Function(String fingerprint)? clientFactory,
    this.scheme = 'https',
    this.requestTimeout = const Duration(seconds: 20),
    this.homeConnectTimeout = const Duration(milliseconds: 2500),
    this.outsideConnectTimeout = const Duration(seconds: 8),
    this.routeMemory,
    this.onExternalChanged,
  }) : _factory = clientFactory ?? createPinnedHttpClient;

  final String? token;

  /// Always `https`; tests with a plain loopback server use `http`.
  final String scheme;
  final Duration requestTimeout;

  /// How long a home network address gets to answer before the next candidate is tried.
  final Duration homeConnectTimeout;
  final Duration outsideConnectTimeout;
  final RouteMemory? routeMemory;

  /// Called after the Mac told, over an authenticated pinned connection, that its external address changed.
  final Future<void> Function(ExternalAddress address)? onExternalChanged;
  final HttpClient Function(String fingerprint) _factory;

  Endpoint _endpoint;
  HttpClient? _client;
  String? _lastGoodHost;
  NetworkRoute? _lastRoute;
  bool _memoryLoaded = false;
  bool _needsSync = true;
  Future<void>? _pendingSync;

  /// The route of the last successful connection; null until one worked.
  final ValueNotifier<NetworkRoute?> route = ValueNotifier(null);

  Endpoint get endpoint => _endpoint;

  /// Uses a new external address (from an address update code) without touching anything else.
  void useExternal(ExternalAddress? address) {
    if (address == null || address == _endpoint.external) return;
    _endpoint = _endpoint.withExternal(address);
  }

  HttpClient get _http => _client ??= _factory(_endpoint.fingerprint);

  /// Closes open connections, streams included.
  void close() {
    _client?.close(force: true);
    _client = null;
  }

  /// Releases what the screens listen to. Call once, when the client is thrown away.
  void dispose() {
    close();
    route.dispose();
  }

  /// Waits for the address check that follows a successful connection (the background check awaits it before it
  /// closes the client).
  Future<void> settle() => _pendingSync ?? Future<void>.value();

  /// Home addresses first, then the external one; the route that worked last goes first.
  @visibleForTesting
  List<Candidate> candidates() {
    final good = _lastGoodHost;
    final hosts = good == null || !_endpoint.hosts.contains(good)
        ? _endpoint.hosts
        : [good, ..._endpoint.hosts.where((host) => host != good)];
    final home = [for (final host in hosts) Candidate(host, _endpoint.port, NetworkRoute.home)];
    final external = _endpoint.external;
    final outside = [if (external != null) Candidate(external.host, external.port, NetworkRoute.outside)];
    return _lastRoute == NetworkRoute.outside ? [...outside, ...home] : [...home, ...outside];
  }

  Future<void> _loadMemory() async {
    if (_memoryLoaded) return;
    _memoryLoaded = true;
    _lastRoute ??= await routeMemory?.read();
  }

  Future<void> _succeeded(Candidate candidate) async {
    if (candidate.route == NetworkRoute.home) _lastGoodHost = candidate.host;
    final changed = _lastRoute != candidate.route;
    _lastRoute = candidate.route;
    route.value = candidate.route;
    if (changed) await routeMemory?.write(candidate.route);
  }

  Duration _connectTimeout(Candidate candidate) =>
      candidate.route == NetworkRoute.home ? homeConnectTimeout : outsideConnectTimeout;

  Uri _uri(Candidate candidate, String path, Map<String, String>? query) =>
      Uri(scheme: scheme, host: candidate.host, port: candidate.port, path: path, queryParameters: query);

  /// After a (re)connection, asks the Mac for its current external address and keeps it when it is a valid,
  /// different one. A failed or malformed answer changes nothing.
  void _learnExternal() {
    if (!_needsSync || token == null || _pendingSync != null) return;
    _needsSync = false;
    final done = _askExternal();
    _pendingSync = done;
    unawaited(done.whenComplete(() => _pendingSync = null));
  }

  Future<void> _askExternal() async {
    try {
      final response = await request('GET', '/remote/v1/endpoint', learn: false);
      final text = JsonObject.of(response.json).optString('ext');
      final learned = ExternalAddress.tryParse(text);
      if (learned == null || learned == _endpoint.external) return;
      _endpoint = _endpoint.withExternal(learned);
      await onExternalChanged?.call(learned);
    } on Object {
      // An older Mac, a bad answer or a lost link: the stored address stays.
    }
  }

  /// Sends one request, trying each candidate address until one answers.
  Future<RemoteResponse> request(
    String method,
    String path, {
    Uint8List? body,
    Map<String, String>? query,
    StepUpHeaders? stepUp,
    bool authenticated = true,
    bool learn = true,
  }) async {
    await _loadMemory();
    for (final candidate in candidates()) {
      try {
        final request = await _http
            .openUrl(method, _uri(candidate, path, query))
            .timeout(_connectTimeout(candidate));
        _decorate(request, authenticated: authenticated, stepUp: stepUp, hasBody: body != null);
        if (body != null) request.add(body);
        final response = await request.close().timeout(requestTimeout);
        _checkCertificate(response);
        final bytes = await _readAll(response).timeout(requestTimeout);
        await _succeeded(candidate);
        if (learn && authenticated) _learnExternal();
        return _checked(RemoteResponse(response.statusCode, bytes));
      } on RemoteFailure {
        rethrow;
      } on TlsException {
        // Not the pinned certificate (or not a Mac at all): never try another address after that.
        throw const RemoteFailure(FailureKind.certificateMismatch);
      } on SocketException {
        continue;
      } on TimeoutException {
        continue;
      } on HttpException {
        continue;
      }
    }
    _needsSync = true;
    throw const RemoteFailure(FailureKind.unreachable);
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
    if (certificate == null || !fingerprintMatches(certificate.der, _endpoint.fingerprint)) {
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
    await _loadMemory();
    for (final candidate in candidates()) {
      HttpClientResponse response;
      try {
        final request = await _http
            .openUrl('GET', _uri(candidate, '/api/assistant/stream', null))
            .timeout(_connectTimeout(candidate));
        _decorate(request, authenticated: true, stepUp: null, hasBody: false);
        request.headers.set(HttpHeaders.acceptHeader, 'text/event-stream');
        response = await request.close().timeout(requestTimeout);
        _checkCertificate(response);
      } on RemoteFailure {
        rethrow;
      } on TlsException {
        throw const RemoteFailure(FailureKind.certificateMismatch);
      } on SocketException {
        continue;
      } on TimeoutException {
        continue;
      } on HttpException {
        continue;
      }
      await _succeeded(candidate);
      _learnExternal();
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
    _needsSync = true;
    throw const RemoteFailure(FailureKind.unreachable);
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
    await request('DELETE', '/remote/v1/device', learn: false);
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
