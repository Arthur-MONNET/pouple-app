import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:pouple_companion/src/data/net/remote_client.dart';
import 'package:pouple_companion/src/data/net/remote_failure.dart';
import 'package:pouple_companion/src/data/net/tunnel_forwarder.dart';

import '../support/fixtures.dart';
import '../support/other_certificate.dart';
import '../support/test_certificate.dart';
import '../support/tunnel_bridge.dart';

SecurityContext _context(String certificate, String key) => SecurityContext()
  ..useCertificateChainBytes(utf8.encode(certificate))
  ..usePrivateKeyBytes(utf8.encode(key));

/// A raw TLS server that counts the application bytes it receives after a completed handshake.
class _RawTls {
  _RawTls._(this.server);

  final SecureServerSocket server;
  int appBytes = 0;
  int handshakes = 0;
  int handshakeErrors = 0;

  static Future<_RawTls> start(String certificate, String key, {bool dropAfterRequest = false}) async {
    final server = await SecureServerSocket.bind(InternetAddress.loopbackIPv4, 0, _context(certificate, key));
    final raw = _RawTls._(server);
    server.listen((socket) {
      raw.handshakes++;
      socket.listen((data) {
        raw.appBytes += data.length;
        if (dropAfterRequest) socket.destroy();
      }, onError: (Object _) {});
    }, onError: (Object _) => raw.handshakeErrors++);
    return raw;
  }

  int get port => server.port;

  Future<void> close() => server.close();
}

/// A TCP server that accepts and closes at once: a handshake cut by the peer.
Future<ServerSocket> _cutting(List<int> accepted) async {
  final server = await ServerSocket.bind(InternetAddress.loopbackIPv4, 0);
  server.listen((socket) {
    accepted.add(1);
    socket.destroy();
  });
  return server;
}

Future<HttpServer> _good(List<String> seen) async {
  final server = await HttpServer.bindSecure(
    InternetAddress.loopbackIPv4,
    0,
    _context(testCertificatePem, testPrivateKeyPem),
  );
  server.listen((request) async {
    seen.add('${request.method} ${request.uri.path}');
    request.response
      ..statusCode = 200
      ..write(jsonEncode(snapshotJson([requestJson()])));
    await request.response.close();
  });
  return server;
}

Future<int> _deadPort() async {
  final server = await ServerSocket.bind(InternetAddress.loopbackIPv4, 0);
  final port = server.port;
  await server.close();
  return port;
}

Matcher _failsWith(FailureKind kind) => throwsA(isA<RemoteFailure>().having((f) => f.kind, 'kind', kind));

void main() {
  RemoteClient client({
    required int homePort,
    ExternalAddress? external,
    String? tunnel,
    Future<TunnelForwarder> Function(String)? forwarder,
  }) => RemoteClient(
    endpoint: Endpoint(
      hosts: const ['127.0.0.1'],
      port: homePort,
      fingerprint: testCertificateFingerprint,
      external: external,
      tunnel: tunnel,
    ),
    token: 'secret-token',
    homeConnectTimeout: const Duration(milliseconds: 500),
    forwarderFactory: forwarder,
  );

  group('a refused certificate sends nothing', () {
    test(
      'direct: no application byte reaches a server with another certificate, and the control one gets some',
      () async {
        final wrong = await _RawTls.start(otherCertificatePem, otherPrivateKeyPem);
        final right = await _RawTls.start(testCertificatePem, testPrivateKeyPem);
        addTearDown(wrong.close);
        addTearDown(right.close);

        final refused = client(homePort: wrong.port);
        addTearDown(refused.dispose);
        await expectLater(refused.snapshot(), _failsWith(FailureKind.certificateMismatch));
        await expectLater(refused.snapshots().toList(), _failsWith(FailureKind.certificateMismatch));
        await Future<void>.delayed(const Duration(milliseconds: 100));
        expect(wrong.appBytes, 0, reason: 'neither the request nor the token may be written');

        final accepted = client(homePort: right.port);
        addTearDown(accepted.dispose);
        // The raw server never answers: the request is written, that is all this control needs.
        unawaited(accepted.snapshot().then((_) {}, onError: (Object _) {}));
        await Future<void>.delayed(const Duration(milliseconds: 300));
        expect(right.appBytes, greaterThan(0));
      },
    );

    test('through the tunnel too', () async {
      final wrong = await _RawTls.start(otherCertificatePem, otherPrivateKeyPem);
      final bridge = await TunnelBridge.start(wrong.port);
      addTearDown(wrong.close);
      addTearDown(bridge.close);
      final forwarders = <TunnelForwarder>[];
      addTearDown(() async {
        for (final f in forwarders) {
          await f.close();
        }
      });
      final remote = client(
        homePort: await _deadPort(),
        tunnel: 'mac.ngrok-free.app',
        forwarder: (domain) async {
          final f = await TunnelForwarder.start(domain, upstream: bridge.uri);
          forwarders.add(f);
          return f;
        },
      );
      addTearDown(remote.dispose);
      await expectLater(remote.snapshot(), _failsWith(FailureKind.certificateMismatch));
      await Future<void>.delayed(const Duration(milliseconds: 100));
      expect(wrong.appBytes, 0);
    });
  });

  group('a handshake cut by the peer is not a pin failure', () {
    test('direct, for a request and for the stream, with or without other addresses', () async {
      final accepted = <int>[];
      final cutting = await _cutting(accepted);
      addTearDown(() => cutting.close());
      final remote = client(homePort: cutting.port);
      addTearDown(remote.dispose);
      await expectLater(remote.snapshot(), _failsWith(FailureKind.unreachable));
      await expectLater(remote.snapshots().toList(), _failsWith(FailureKind.unreachable));
      expect(accepted, isNotEmpty);

      final withExternal = client(
        homePort: cutting.port,
        external: ExternalAddress('127.0.0.1', cutting.port),
      );
      addTearDown(withExternal.dispose);
      await expectLater(withExternal.snapshot(), _failsWith(FailureKind.unreachable));
    });

    test('through the tunnel: the peer drops the handshake', () async {
      final accepted = <int>[];
      final cutting = await _cutting(accepted);
      final bridge = await TunnelBridge.start(cutting.port);
      addTearDown(() => cutting.close());
      addTearDown(bridge.close);
      final forwarders = <TunnelForwarder>[];
      addTearDown(() async {
        for (final f in forwarders) {
          await f.close();
        }
      });
      final remote = client(
        homePort: await _deadPort(),
        tunnel: 'mac.ngrok-free.app',
        forwarder: (domain) async {
          final f = await TunnelForwarder.start(domain, upstream: bridge.uri);
          forwarders.add(f);
          return f;
        },
      );
      addTearDown(remote.dispose);
      await expectLater(remote.snapshot(), _failsWith(FailureKind.unreachable));
      await expectLater(remote.snapshots().toList(), _failsWith(FailureKind.unreachable));
    });

    test('a refusal earlier on an endpoint does not turn a later cut into a pin failure', () async {
      // The same client talks to a server that first presents another certificate, then is replaced by one that
      // cuts: the first answer is a mismatch, the second only unreachable.
      final wrong = await _RawTls.start(otherCertificatePem, otherPrivateKeyPem);
      final port = wrong.port;
      final remote = client(homePort: port);
      addTearDown(remote.dispose);
      await expectLater(remote.snapshot(), _failsWith(FailureKind.certificateMismatch));
      await wrong.close();
      final server = await ServerSocket.bind(InternetAddress.loopbackIPv4, port);
      addTearDown(() => server.close());
      server.listen((socket) => socket.destroy());
      await expectLater(remote.snapshot(), _failsWith(FailureKind.unreachable));
    });
  });

  group('a request is not sent twice', () {
    test(
      'a POST whose connection drops after it was sent is reported, not retried on the next address',
      () async {
        final dropping = await _RawTls.start(testCertificatePem, testPrivateKeyPem, dropAfterRequest: true);
        final seen = <String>[];
        final next = await _good(seen);
        addTearDown(dropping.close);
        addTearDown(() => next.close(force: true));
        final remote = client(homePort: dropping.port, external: ExternalAddress('127.0.0.1', next.port));
        addTearDown(remote.dispose);
        await expectLater(
          remote.request('POST', '/remote/v1/challenge'),
          throwsA(
            isA<RemoteFailure>()
                .having((f) => f.kind, 'kind', FailureKind.unreachable)
                .having((f) => f.display, 'display', contains('may or may not')),
          ),
        );
        expect(dropping.appBytes, greaterThan(0));
        expect(seen, isEmpty, reason: 'the next address must never see the request');
      },
    );

    test('a POST that could not connect still moves on to the next address', () async {
      final seen = <String>[];
      final next = await _good(seen);
      addTearDown(() => next.close(force: true));
      final remote = client(homePort: await _deadPort(), external: ExternalAddress('127.0.0.1', next.port));
      addTearDown(remote.dispose);
      final response = await remote.request('POST', '/remote/v1/challenge', body: Uint8List(0));
      expect(response.status, 200);
      expect(seen, ['POST /remote/v1/challenge']);
    });

    test('a GET is read-only, so it may be sent again to the next address', () async {
      final dropping = await _RawTls.start(testCertificatePem, testPrivateKeyPem, dropAfterRequest: true);
      final seen = <String>[];
      final next = await _good(seen);
      addTearDown(dropping.close);
      addTearDown(() => next.close(force: true));
      final remote = client(homePort: dropping.port, external: ExternalAddress('127.0.0.1', next.port));
      addTearDown(remote.dispose);
      expect((await remote.snapshot()).requests, hasLength(1));
      expect(seen, ['GET /api/assistant']);
    });
  });
}
