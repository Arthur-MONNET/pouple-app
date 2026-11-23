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
import 'tunnel_domain.dart';
import 'tunnel_forwarder.dart';

export 'external_address.dart';
export 'route_memory.dart';
export 'tunnel_domain.dart';

/// Where the Mac listens and the certificate to pin.
class Endpoint {
  const Endpoint({
    required this.hosts,
    required this.port,
    required this.fingerprint,
    this.external,
    this.tunnel,
  });

  /// Home network addresses to try in order. An IPv6 address has no brackets here.
  final List<String> hosts;
  final int port;
  final String fingerprint;

  /// Where the router exposes the Mac to the Internet, if it does.
  final ExternalAddress? external;

  /// The ngrok domain the Mac is reachable through (`wss://<domain>/tunnel/v1`), if it has one.
  final String? tunnel;

  Endpoint withExternal(ExternalAddress? address) =>
      Endpoint(hosts: hosts, port: port, fingerprint: fingerprint, external: address, tunnel: tunnel);

  Endpoint withTunnel(String? domain) =>
      Endpoint(hosts: hosts, port: port, fingerprint: fingerprint, external: external, tunnel: domain);
}

/// The host name of every tunnel URL. It is never resolved: the connection factory sends it to the forwarder, and
/// the pinning looks at the certificate, not at the name.
const tunnelPlaceholderHost = 'mac.invalid';

/// One address to try, and which way it goes. The pinned fingerprint is the same for every candidate. For the
/// tunnel, [host] is the ngrok domain and [port] 443: the connection really goes to the forwarder's Unix socket.
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

/// What happened on each candidate of one call, to name the failure when none worked.
class _Attempts {
  _Attempts(this._endpoint);

  final Endpoint _endpoint;
  int _homePinFailures = 0;
  int _homeUnreachable = 0;
  bool _externalPinFailed = false;

  void pinFailed(Candidate candidate) {
    if (candidate.route == NetworkRoute.home) {
      _homePinFailures++;
    } else {
      _externalPinFailed = true;
    }
  }

  void unreachable(Candidate candidate) {
    if (candidate.route == NetworkRoute.home) _homeUnreachable++;
  }

  /// A wrong certificate on the external address is a real alarm. On the home network it is usually another
  /// device at the same address on a foreign network, so it only counts when nothing else could be tried.
  RemoteFailure failure() {
    final onlyHomeAnswered =
        _endpoint.external == null &&
        _endpoint.tunnel == null &&
        _homePinFailures > 0 &&
        _homeUnreachable == 0;
    if (_externalPinFailed || onlyHomeAnswered) return const RemoteFailure(FailureKind.certificateMismatch);
    return const RemoteFailure(FailureKind.unreachable);
  }
}

/// The Mac's phone API over TLS with a pinned certificate.
///
/// Tries the home network addresses first (with a short timeout), then the external address, and remembers which
/// route worked. A certificate that is not the pinned one skips that address (nothing is sent to it) and the next one
/// is tried; see [_Attempts.failure] for when it is reported. Never logs a token, a body, an answer or an address.
class RemoteClient {
  RemoteClient({
    required this._endpoint,
    this.token,
    HttpClient Function(String fingerprint)? clientFactory,
    this.scheme = 'https',
    this.requestTimeout = const Duration(seconds: 20),
    this.homeConnectTimeout = const Duration(milliseconds: 2500),
    this.outsideConnectTimeout = const Duration(seconds: 8),
    this.tunnelConnectTimeout = const Duration(seconds: 15),
    this.routeMemory,
    this.onExternalChanged,
    this.onTunnelChanged,
    Future<TunnelForwarder> Function(String domain)? forwarderFactory,
  }) : _customFactory = clientFactory,
       _forwarderFactory = forwarderFactory ?? TunnelForwarder.start;

  final String? token;

  /// Always `https`; tests with a plain loopback server use `http`.
  final String scheme;
  final Duration requestTimeout;

  /// How long a home network address gets to answer before the next candidate is tried.
  final Duration homeConnectTimeout;
  final Duration outsideConnectTimeout;

  /// The tunnel gets longer: the WebSocket to ngrok opens before the TLS handshake can start.
  final Duration tunnelConnectTimeout;
  final RouteMemory? routeMemory;

  /// Called after the Mac told, over an authenticated pinned connection, that its external address changed.
  final Future<void> Function(ExternalAddress? address)? onExternalChanged;

  /// Called after the Mac told, over an authenticated pinned connection, that its tunnel domain changed.
  final Future<void> Function(String? domain)? onTunnelChanged;
  final HttpClient Function(String fingerprint)? _customFactory;
  final Future<TunnelForwarder> Function(String domain) _forwarderFactory;

  Endpoint _endpoint;
  HttpClient? _client;
  HttpClient? _tunnelClient;
  String? _lastGoodHost;
  NetworkRoute? _lastRoute;
  bool _memoryLoaded = false;
  bool _needsSync = true;
  Future<void>? _pendingSync;
  Future<TunnelForwarder>? _forwarder;
  String? _forwarderDomain;

  /// The route of the last successful connection; null until one worked.
  final ValueNotifier<NetworkRoute?> route = ValueNotifier(null);

  Endpoint get endpoint => _endpoint;

  /// Uses a new external address (from an address update code) without touching anything else.
  void useExternal(ExternalAddress? address) {
    if (address == null || address == _endpoint.external) return;
    _endpoint = _endpoint.withExternal(address);
  }

  /// Uses a new tunnel domain (from an address update code) without touching anything else.
  void useTunnel(String? domain) {
    if (domain == null || domain == _endpoint.tunnel) return;
    _endpoint = _endpoint.withTunnel(domain);
    _stopForwarder();
  }

  /// The endpoints (`host:port`, as the HTTP client names them) whose certificate the pin callback refused, with the
  /// order number of the refusal. A TLS error counts as a pin failure only if its endpoint was refused after the
  /// attempt began: a handshake cut by the peer never reaches the callback.
  final Map<String, int> _refusedAt = {};
  int _refusals = 0;

  void _recordRefused(String host, int port) => _refusedAt['$host:$port'] = ++_refusals;

  HttpClient get _http => _client ??= (_customFactory ?? _pinned)(_endpoint.fingerprint);

  HttpClient _pinned(String fingerprint) => createPinnedHttpClient(fingerprint, onRefused: _recordRefused);

  /// The client of the tunnel route. It is separate from [_http], which keeps its direct, pinned connections exactly
  /// as they are: the HTTP client takes the socket of a `connectionFactory` as it comes and applies neither its
  /// context nor its certificate callback to it, so this client's factory does the pinned TLS handshake itself
  /// (see [pinnedSecureTask]), with the same pin and the same refusal record.
  HttpClient get _tunnelHttp =>
      _tunnelClient ??= (_customFactory?.call(_endpoint.fingerprint) ?? HttpClient())
        ..connectionTimeout = const Duration(seconds: 10)
        ..idleTimeout = const Duration(seconds: 30)
        ..findProxy = ((_) => 'DIRECT')
        ..connectionFactory = _connectTunnel;

  /// The tunnel placeholder URL goes to the forwarder's Unix socket, never to the network.
  Future<ConnectionTask<Socket>> _connectTunnel(Uri uri, String? proxyHost, int? proxyPort) async {
    final domain = _endpoint.tunnel;
    if (domain == null) throw const SocketException('No tunnel.');
    final forwarder = await _forwarderFor(domain);
    final task = await Socket.startConnect(forwarder.address, 0);
    if (scheme != 'https') return task;
    return pinnedSecureTask(task, _endpoint.fingerprint, onRefused: () => _recordRefused(uri.host, uri.port));
  }

  HttpClient _clientFor(Candidate candidate) => candidate.route == NetworkRoute.tunnel ? _tunnelHttp : _http;

  /// Closes open connections, streams included.
  void close() {
    _client?.close(force: true);
    _client = null;
    _tunnelClient?.close(force: true);
    _tunnelClient = null;
    _stopForwarder();
  }

  void _stopForwarder() {
    final forwarder = _forwarder;
    _forwarder = null;
    _forwarderDomain = null;
    if (forwarder != null) unawaited(forwarder.then((f) => f.close(), onError: (_) {}));
  }

  /// The loopback forwarder of the current tunnel domain, started on first use.
  Future<TunnelForwarder> _forwarderFor(String domain) {
    if (_forwarderDomain != domain) _stopForwarder();
    _forwarderDomain = domain;
    final started = _forwarder ??= _forwarderFactory(domain);
    // A forwarder that could not start is not kept: the next call tries again.
    started.then(
      (_) {},
      onError: (_) {
        if (identical(_forwarder, started)) {
          _forwarder = null;
          _forwarderDomain = null;
        }
      },
    );
    return started;
  }

  /// Releases what the screens listen to. Call once, when the client is thrown away.
  void dispose() {
    close();
    route.dispose();
  }

  /// Waits for the address check that follows a successful connection (the background check awaits it before it
  /// closes the client).
  Future<void> settle() => _pendingSync ?? Future<void>.value();

  /// Home addresses, then the external one, then the tunnel; the route that worked last goes first.
  @visibleForTesting
  List<Candidate> candidates() {
    final good = _lastGoodHost;
    final hosts = good == null || !_endpoint.hosts.contains(good)
        ? _endpoint.hosts
        : [good, ..._endpoint.hosts.where((host) => host != good)];
    final home = [for (final host in hosts) Candidate(host, _endpoint.port, NetworkRoute.home)];
    final external = _endpoint.external;
    final tunnel = _endpoint.tunnel;
    final ordered = [
      ...home,
      if (external != null) Candidate(external.host, external.port, NetworkRoute.outside),
      if (tunnel != null) Candidate(tunnel, 443, NetworkRoute.tunnel),
    ];
    final remembered = _lastRoute;
    if (remembered == null || remembered == NetworkRoute.home) return ordered;
    return [...ordered.where((c) => c.route == remembered), ...ordered.where((c) => c.route != remembered)];
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

  Duration _connectTimeout(Candidate candidate) => switch (candidate.route) {
    NetworkRoute.home => homeConnectTimeout,
    NetworkRoute.outside => outsideConnectTimeout,
    NetworkRoute.tunnel => tunnelConnectTimeout,
  };

  /// The URL of [candidate]. The tunnel's is a placeholder: the HTTP client's connection factory sends it to the
  /// forwarder, and the pin check never looks at the host name.
  Uri _uri(Candidate candidate, String path, Map<String, String>? query) {
    final tunnel = candidate.route == NetworkRoute.tunnel;
    return Uri(
      scheme: scheme,
      host: tunnel ? tunnelPlaceholderHost : candidate.host,
      port: tunnel ? 443 : candidate.port,
      path: path,
      queryParameters: query,
    );
  }

  /// Counts a TLS error of [uri]: a refused certificate is a pin failure, any other handshake error (cut by the
  /// peer, a proxy, a captive portal, the tunnel dropping) is just an address that does not answer.
  void _tlsFailed(_Attempts attempts, Candidate candidate, Uri uri, int startedAt) {
    if ((_refusedAt['${uri.host}:${uri.port}'] ?? 0) > startedAt) {
      attempts.pinFailed(candidate);
    } else {
      attempts.unreachable(candidate);
    }
  }

  /// After a (re)connection, asks the Mac for its current external address and tunnel domain and keeps each one
  /// that is valid and different. A failed or malformed answer (or field) changes nothing.
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
      final raw = response.json;
      final json = JsonObject.of(raw);
      final fields = raw as Map;
      // A key that is absent (an older Mac) or malformed changes nothing; an explicit null means "none any more".
      if (fields.containsKey('ext') && fields['ext'] == null) {
        if (_endpoint.external != null) {
          _endpoint = _endpoint.withExternal(null);
          await onExternalChanged?.call(null);
        }
      } else {
        final learned = ExternalAddress.tryParse(_optText(json, 'ext'));
        if (learned != null && learned != _endpoint.external) {
          _endpoint = _endpoint.withExternal(learned);
          await onExternalChanged?.call(learned);
        }
      }
      if (fields.containsKey('tun') && fields['tun'] == null) {
        if (_endpoint.tunnel != null) {
          _endpoint = _endpoint.withTunnel(null);
          _stopForwarder();
          await onTunnelChanged?.call(null);
        }
      } else {
        final tunnel = parseTunnelDomain(_optText(json, 'tun'));
        if (tunnel != null && tunnel != _endpoint.tunnel) {
          useTunnel(tunnel);
          await onTunnelChanged?.call(tunnel);
        }
      }
    } on Object {
      // An older Mac, a bad answer or a lost link: the stored address stays.
    }
  }

  static String? _optText(JsonObject json, String key) {
    try {
      return json.optString(key);
    } on FormatException {
      return null;
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
    final attempts = _Attempts(_endpoint);
    // A GET changes nothing on the Mac, so it may be sent again to the next address. Anything else is sent at most
    // once: after its bytes may have left, a failure is reported, not retried (it could run twice).
    final repeatable = method == 'GET' || method == 'HEAD';
    for (final candidate in candidates()) {
      final uri = _uri(candidate, path, query);
      final startedAt = _refusals;
      final HttpClientRequest request;
      try {
        request = await _clientFor(candidate).openUrl(method, uri).timeout(_connectTimeout(candidate));
      } on TlsException {
        _tlsFailed(attempts, candidate, uri, startedAt);
        continue;
      } on SocketException {
        attempts.unreachable(candidate);
        continue;
      } on TimeoutException {
        attempts.unreachable(candidate);
        continue;
      } on HttpException {
        attempts.unreachable(candidate);
        continue;
      }
      try {
        _decorate(request, authenticated: authenticated, stepUp: stepUp, hasBody: body != null);
        if (body != null) request.add(body);
        final response = await request.close().timeout(requestTimeout);
        _checkCertificate(response);
        final bytes = await _readAll(response).timeout(requestTimeout);
        await _succeeded(candidate);
        if (learn && authenticated) _learnExternal();
        return _checked(RemoteResponse(response.statusCode, bytes));
      } on RemoteFailure catch (failure) {
        if (failure.kind != FailureKind.certificateMismatch || !repeatable) rethrow;
        attempts.pinFailed(candidate);
      } on Object catch (error) {
        if (error is! TlsException &&
            error is! SocketException &&
            error is! TimeoutException &&
            error is! HttpException) {
          rethrow;
        }
        if (!repeatable) {
          throw const RemoteFailure(
            FailureKind.unreachable,
            message:
                'The connection dropped while the request was being sent. The Mac may or may not have '
                'received it: check before trying again.',
          );
        }
        attempts.unreachable(candidate);
      }
    }
    _needsSync = true;
    throw attempts.failure();
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

  /// A consistency check on the answer, not the guard. The guard is the certificate callback of the pinned client:
  /// a handshake with any other certificate fails before a byte of the request (the token included) is sent.
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
    final attempts = _Attempts(_endpoint);
    for (final candidate in candidates()) {
      HttpClientResponse response;
      final uri = _uri(candidate, '/api/assistant/stream', null);
      final startedAt = _refusals;
      try {
        final request = await _clientFor(candidate).openUrl('GET', uri).timeout(_connectTimeout(candidate));
        _decorate(request, authenticated: true, stepUp: null, hasBody: false);
        request.headers.set(HttpHeaders.acceptHeader, 'text/event-stream');
        response = await request.close().timeout(requestTimeout);
        _checkCertificate(response);
      } on RemoteFailure catch (failure) {
        if (failure.kind != FailureKind.certificateMismatch) rethrow;
        attempts.pinFailed(candidate);
        continue;
      } on TlsException {
        _tlsFailed(attempts, candidate, uri, startedAt);
        continue;
      } on SocketException {
        attempts.unreachable(candidate);
        continue;
      } on TimeoutException {
        attempts.unreachable(candidate);
        continue;
      } on HttpException {
        attempts.unreachable(candidate);
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
    throw attempts.failure();
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
  /// GET /remote/v1/endpoint, authenticated, without learning from the answer: succeeds only if an address answers
  /// with the pinned certificate and accepts this phone's token. Used to check an address before it is stored.
  Future<void> verify() async {
    await request('GET', '/remote/v1/endpoint', learn: false);
  }

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
