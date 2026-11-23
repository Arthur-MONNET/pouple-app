import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:pouple_companion/src/data/net/remote_client.dart';
import 'package:pouple_companion/src/data/net/remote_failure.dart';
import 'package:pouple_companion/src/data/net/tunnel_forwarder.dart';

import '../support/fake_mac.dart';
import '../support/fixtures.dart';
import '../support/other_certificate.dart';
import '../support/test_certificate.dart';
import '../support/tunnel_bridge.dart';

const _domain = 'mac.ngrok-free.app';

class _Memory implements RouteMemory {
  _Memory([this.value]);

  NetworkRoute? value;

  @override
  Future<NetworkRoute?> read() async => value;

  @override
  Future<void> write(NetworkRoute route) async => value = route;
}

Future<HttpServer> _tlsServer(String certificate, String key, List<String> seen) async {
  final context = SecurityContext()
    ..useCertificateChainBytes(utf8.encode(certificate))
    ..usePrivateKeyBytes(utf8.encode(key));
  final server = await HttpServer.bindSecure(InternetAddress.loopbackIPv4, 0, context);
  server.listen((request) async {
    seen.add('${request.method} ${request.uri.path} ${request.headers.value('authorization')}');
    if (request.uri.path == '/api/assistant/stream') {
      request.response.headers.contentType = ContentType('text', 'event-stream');
      request.response.write('event: snapshot\ndata: ${jsonEncode(snapshotJson([requestJson()]))}\n\n');
    } else {
      request.response
        ..statusCode = 200
        ..write(jsonEncode(snapshotJson([requestJson()])));
    }
    await request.response.close();
  });
  return server;
}

Future<int> _deadPort() async {
  final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
  final port = server.port;
  await server.close();
  return port;
}

Matcher _failsWith(FailureKind kind) => throwsA(isA<RemoteFailure>().having((f) => f.kind, 'kind', kind));

void main() {
  group('candidate order', () {
    RemoteClient client({NetworkRoute? remembered, bool external = true, bool tunnel = true}) => RemoteClient(
      endpoint: Endpoint(
        hosts: const ['192.168.1.20', '192.168.1.21'],
        port: 47821,
        fingerprint: 'unused',
        external: external ? ExternalAddress('mac.example.org', 8443) : null,
        tunnel: tunnel ? _domain : null,
      ),
      token: 'tok',
    );

    List<NetworkRoute> routes(RemoteClient c) => c.candidates().map((x) => x.route).toList();

    test('home addresses, then the external address, then the tunnel', () {
      final remote = client();
      addTearDown(remote.dispose);
      expect(routes(remote), [
        NetworkRoute.home,
        NetworkRoute.home,
        NetworkRoute.outside,
        NetworkRoute.tunnel,
      ]);
      final tunnel = remote.candidates().last;
      expect((tunnel.host, tunnel.port), (_domain, 443));
    });

    test('the tunnel is a candidate with no external address too, and absent when there is none', () {
      final only = client(external: false);
      addTearDown(only.dispose);
      expect(routes(only), [NetworkRoute.home, NetworkRoute.home, NetworkRoute.tunnel]);
      final none = client(tunnel: false);
      addTearDown(none.dispose);
      expect(routes(none), [NetworkRoute.home, NetworkRoute.home, NetworkRoute.outside]);
    });

    test('learning or using a tunnel domain adds the candidate', () {
      final remote = client(tunnel: false);
      addTearDown(remote.dispose);
      remote.useTunnel(_domain);
      expect(remote.endpoint.tunnel, _domain);
      expect(routes(remote).last, NetworkRoute.tunnel);
    });
  });

  group('the pinned client through the tunnel', () {
    late List<String> seen;
    late HttpServer mac;
    late TunnelBridge bridge;
    late List<TunnelForwarder> forwarders;

    Future<void> startMac(String certificate, String key) async {
      mac = await _tlsServer(certificate, key, seen);
      bridge = await TunnelBridge.start(mac.port);
    }

    setUp(() {
      seen = [];
      forwarders = [];
    });
    tearDown(() async {
      for (final forwarder in forwarders) {
        await forwarder.close();
      }
      await bridge.close();
      await mac.close(force: true);
    });

    RemoteClient client({
      int? homePort,
      ExternalAddress? external,
      RouteMemory? memory,
      String pin = testCertificateFingerprint,
      Future<void> Function(String?)? onTunnel,
    }) => RemoteClient(
      endpoint: Endpoint(
        hosts: const ['127.0.0.1'],
        port: homePort ?? 1,
        fingerprint: pin,
        external: external,
        tunnel: _domain,
      ),
      token: 'tok',
      routeMemory: memory,
      homeConnectTimeout: const Duration(milliseconds: 500),
      onTunnelChanged: onTunnel,
      forwarderFactory: (domain) async {
        expect(domain, _domain);
        final forwarder = await TunnelForwarder.start(domain, upstream: bridge.uri);
        forwarders.add(forwarder);
        return forwarder;
      },
    );

    test('falls through home and the external address to the tunnel, with the same pin', () async {
      await startMac(testCertificatePem, testPrivateKeyPem);
      final memory = _Memory();
      final remote = client(
        homePort: await _deadPort(),
        external: ExternalAddress('127.0.0.1', await _deadPort()),
        memory: memory,
      );
      addTearDown(remote.dispose);
      expect((await remote.snapshot()).requests, hasLength(1));
      expect(remote.route.value, NetworkRoute.tunnel);
      expect(memory.value, NetworkRoute.tunnel);
      expect(seen, ['GET /api/assistant Bearer tok']);
      expect(bridge.skipHeaders, ['1']);
    });

    test('the stream goes through the tunnel too', () async {
      await startMac(testCertificatePem, testPrivateKeyPem);
      final remote = client(homePort: await _deadPort());
      addTearDown(remote.dispose);
      expect(await remote.snapshots().toList(), hasLength(1));
      expect(remote.route.value, NetworkRoute.tunnel);
    });

    test('requests of one client share one forwarder, which closes with the client', () async {
      await startMac(testCertificatePem, testPrivateKeyPem);
      final remote = client(homePort: await _deadPort());
      await remote.snapshot();
      await remote.snapshot();
      expect(forwarders, hasLength(1));
      final address = forwarders.single.address;
      remote.dispose();
      await Future<void>.delayed(const Duration(milliseconds: 200));
      await expectLater(Socket.connect(address, 0), throwsA(isA<SocketException>()));
    });

    test('a changed tunnel domain replaces the forwarder', () async {
      await startMac(testCertificatePem, testPrivateKeyPem);
      final remote = client(homePort: await _deadPort());
      addTearDown(remote.dispose);
      await remote.snapshot();
      final first = forwarders.single.address;
      remote.useTunnel('other.ngrok-free.app');
      await Future<void>.delayed(const Duration(milliseconds: 200));
      await expectLater(Socket.connect(first, 0), throwsA(isA<SocketException>()));
    });

    test('the remembered tunnel route is tried first, ahead of a home address that would answer', () async {
      await startMac(testCertificatePem, testPrivateKeyPem);
      final homeSeen = <String>[];
      final home = await _tlsServer(testCertificatePem, testPrivateKeyPem, homeSeen);
      addTearDown(() => home.close(force: true));
      final remote = client(homePort: home.port, memory: _Memory(NetworkRoute.tunnel));
      addTearDown(remote.dispose);
      await remote.snapshot();
      expect(remote.route.value, NetworkRoute.tunnel);
      expect(homeSeen, isEmpty);
      expect(seen, hasLength(1));
    });

    test('a reachable home address means the tunnel is never opened', () async {
      await startMac(testCertificatePem, testPrivateKeyPem);
      final homeSeen = <String>[];
      final home = await _tlsServer(testCertificatePem, testPrivateKeyPem, homeSeen);
      addTearDown(() => home.close(force: true));
      final remote = client(homePort: home.port);
      addTearDown(remote.dispose);
      await remote.snapshot();
      expect(remote.route.value, NetworkRoute.home);
      expect(forwarders, isEmpty);
      expect(bridge.opened, 0);
    });

    test('a wrong pin on the tunnel is a hard certificateMismatch and nothing is sent', () async {
      await startMac(otherCertificatePem, otherPrivateKeyPem);
      final remote = client(homePort: await _deadPort());
      addTearDown(remote.dispose);
      await expectLater(remote.snapshot(), _failsWith(FailureKind.certificateMismatch));
      await expectLater(remote.snapshots().toList(), _failsWith(FailureKind.certificateMismatch));
      expect(seen, isEmpty);
    });

    test(
      'a wrong pin on the tunnel counts like the external endpoint: it wins over an unreachable one',
      () async {
        await startMac(otherCertificatePem, otherPrivateKeyPem);
        final remote = client(
          homePort: await _deadPort(),
          external: ExternalAddress('127.0.0.1', await _deadPort()),
        );
        addTearDown(remote.dispose);
        await expectLater(remote.snapshot(), _failsWith(FailureKind.certificateMismatch));
      },
    );

    test('a wrong home certificate does not hide a good tunnel', () async {
      await startMac(testCertificatePem, testPrivateKeyPem);
      final foreign = await _tlsServer(otherCertificatePem, otherPrivateKeyPem, []);
      addTearDown(() => foreign.close(force: true));
      final remote = client(homePort: foreign.port);
      addTearDown(remote.dispose);
      expect((await remote.snapshot()).requests, hasLength(1));
      expect(remote.route.value, NetworkRoute.tunnel);
    });

    test('a tunnel that is down is unreachable, not a certificate problem', () async {
      await startMac(testCertificatePem, testPrivateKeyPem);
      await bridge.close();
      final remote = client(homePort: await _deadPort());
      addTearDown(remote.dispose);
      await expectLater(remote.snapshot(), _failsWith(FailureKind.unreachable));
      await expectLater(remote.snapshots().toList(), _failsWith(FailureKind.unreachable));
      expect(seen, isEmpty);
    });

    test(
      'a failing tunnel moves on to the candidates after it, and a down remembered tunnel to home',
      () async {
        await startMac(testCertificatePem, testPrivateKeyPem);
        await bridge.close();
        final homeSeen = <String>[];
        final home = await _tlsServer(testCertificatePem, testPrivateKeyPem, homeSeen);
        addTearDown(() => home.close(force: true));
        final memory = _Memory(NetworkRoute.tunnel);
        final remote = client(homePort: home.port, memory: memory);
        addTearDown(remote.dispose);
        await remote.snapshot();
        expect(remote.route.value, NetworkRoute.home);
        expect(memory.value, NetworkRoute.home);
      },
    );
  });

  group('learning the tunnel domain from the Mac', () {
    late FakeMac home;

    setUp(() async {
      home = await FakeMac.start();
      home.answers['GET /remote/v1/device'] = (200, {'id': 'x'});
    });
    tearDown(() => home.close());

    Future<(RemoteClient, List<String?>, List<ExternalAddress?>)> connect(String? stored) async {
      final tunnels = <String?>[];
      final externals = <ExternalAddress?>[];
      final remote = RemoteClient(
        endpoint: Endpoint(
          hosts: const ['127.0.0.1'],
          port: home.port,
          fingerprint: 'unused',
          tunnel: stored,
        ),
        token: 'tok',
        scheme: 'http',
        onTunnelChanged: (domain) async => tunnels.add(domain),
        onExternalChanged: (address) async => externals.add(address),
      );
      addTearDown(remote.dispose);
      await remote.checkDevice();
      await remote.settle();
      return (remote, tunnels, externals);
    }

    test('adopts a valid domain when none was stored, and reports it', () async {
      home.answers['GET /remote/v1/endpoint'] = (200, {'ext': null, 'tun': 'New.ngrok-free.app'});
      final (remote, tunnels, _) = await connect(null);
      expect(remote.endpoint.tunnel, 'new.ngrok-free.app');
      expect(tunnels, ['new.ngrok-free.app']);
      expect(remote.candidates().last.route, NetworkRoute.tunnel);
    });

    test('replaces a different stored domain', () async {
      home.answers['GET /remote/v1/endpoint'] = (200, {'tun': 'new.ngrok-free.app'});
      final (remote, tunnels, _) = await connect('old.ngrok-free.app');
      expect(remote.endpoint.tunnel, 'new.ngrok-free.app');
      expect(tunnels, hasLength(1));
    });

    test('learns the external address and the tunnel from the same answer', () async {
      home.answers['GET /remote/v1/endpoint'] = (
        200,
        {'ext': 'mac.example.org:8443', 'tun': 'new.ngrok-free.app'},
      );
      final (remote, tunnels, externals) = await connect(null);
      expect(remote.endpoint.external.toString(), 'mac.example.org:8443');
      expect(remote.endpoint.tunnel, 'new.ngrok-free.app');
      expect(tunnels, hasLength(1));
      expect(externals, hasLength(1));
    });

    test('an explicit null clears the stored domain and reports it; an absent key changes nothing', () async {
      home.answers['GET /remote/v1/endpoint'] = (200, {'ext': null, 'tun': null});
      final (remote, tunnels, _) = await connect('old.ngrok-free.app');
      expect(remote.endpoint.tunnel, isNull);
      expect(tunnels, [null]);
      expect(remote.candidates().every((c) => c.route != NetworkRoute.tunnel), isTrue);
    });

    test('the same domain changes nothing', () async {
      home.answers['GET /remote/v1/endpoint'] = (200, {'tun': 'old.ngrok-free.app'});
      final (_, tunnels, _) = await connect('old.ngrok-free.app');
      expect(tunnels, isEmpty);
    });

    for (final entry in <String, (int, Object?)>{
      'an old Mac that only answers ext': (200, {'ext': null}),
      'a scheme': (200, {'tun': 'https://new.ngrok-free.app'}),
      'a port': (200, {'tun': 'new.ngrok-free.app:443'}),
      'a path': (200, {'tun': 'new.ngrok-free.app/tunnel/v1'}),
      'an IP address': (200, {'tun': '203.0.113.7'}),
      'a wrong type': (200, {'tun': 42}),
      'an answer that is not JSON': (200, 'hello'),
      'an old Mac that has no such route': (404, {'error': 'Not found.'}),
      'a server error': (500, null),
    }.entries) {
      test('ignores ${entry.key}', () async {
        home.answers['GET /remote/v1/endpoint'] = entry.value;
        final (remote, tunnels, _) = await connect('old.ngrok-free.app');
        expect(remote.endpoint.tunnel, 'old.ngrok-free.app');
        expect(tunnels, isEmpty);
      });
    }

    test('a bad tunnel does not stop a good external address from being learned', () async {
      home.answers['GET /remote/v1/endpoint'] = (200, {'ext': 'mac.example.org:8443', 'tun': 'bad value'});
      final (remote, tunnels, externals) = await connect(null);
      expect(remote.endpoint.external.toString(), 'mac.example.org:8443');
      expect(remote.endpoint.tunnel, isNull);
      expect(tunnels, isEmpty);
      expect(externals, hasLength(1));
    });
  });
}
