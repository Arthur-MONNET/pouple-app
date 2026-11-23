import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:pouple_companion/src/data/net/remote_client.dart';
import 'package:pouple_companion/src/data/net/remote_failure.dart';

import '../support/fake_mac.dart';
import '../support/fixtures.dart';
import '../support/other_certificate.dart';
import '../support/test_certificate.dart';

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
    seen.add(request.uri.path);
    request.response
      ..statusCode = 200
      ..write(jsonEncode(snapshotJson([requestJson()])));
    await request.response.close();
  });
  return server;
}

void main() {
  late FakeMac home;
  late FakeMac outside;

  setUp(() async {
    home = await FakeMac.start();
    outside = await FakeMac.start();
    home.answers['GET /remote/v1/device'] = (200, {'id': 'x'});
    outside.answers['GET /remote/v1/device'] = (200, {'id': 'x'});
  });
  tearDown(() async {
    await home.close();
    await outside.close();
  });

  ExternalAddress ext(FakeMac mac) => ExternalAddress('localhost', mac.port);

  RemoteClient client({
    required ExternalAddress? external,
    int? homePort,
    RouteMemory? memory,
    Future<void> Function(ExternalAddress?)? onChanged,
  }) => RemoteClient(
    endpoint: Endpoint(
      hosts: const ['127.0.0.1'],
      port: homePort ?? home.port,
      fingerprint: 'unused',
      external: external,
    ),
    token: 'tok',
    scheme: 'http',
    routeMemory: memory,
    onExternalChanged: onChanged,
    homeConnectTimeout: const Duration(milliseconds: 500),
  );

  group('ordering and fallback', () {
    test('tries the home network first, then the external address', () {
      final remote = client(external: ext(outside));
      addTearDown(remote.dispose);
      final order = remote.candidates();
      expect(order.map((c) => c.route), [NetworkRoute.home, NetworkRoute.outside]);
      expect(order.last.port, outside.port);
    });

    test('with both reachable, the home network answers and the external address is not touched', () async {
      final remote = client(external: ext(outside));
      addTearDown(remote.dispose);
      await remote.checkDevice();
      expect(home.seen, hasLength(1));
      expect(outside.seen, isEmpty);
      expect(remote.route.value, NetworkRoute.home);
    });

    test('falls over to the external address when the home network does not answer', () async {
      final deadPort = home.port;
      await home.close();
      final remote = client(external: ext(outside), homePort: deadPort);
      addTearDown(remote.dispose);
      await remote.checkDevice();
      expect(outside.seen, hasLength(1));
      expect(remote.route.value, NetworkRoute.outside);
      expect(remote.candidates().first.route, NetworkRoute.outside);
      home = await FakeMac.start();
    });

    test('remembers the route that worked, across clients, and tries it first', () async {
      final memory = _Memory();
      final deadPort = home.port;
      await home.close();
      final first = client(external: ext(outside), homePort: deadPort, memory: memory);
      addTearDown(first.dispose);
      await first.checkDevice();
      expect(memory.value, NetworkRoute.outside);

      // Home is back: a fresh client (the background check) still starts with the external address.
      home = await FakeMac.start();
      home.answers['GET /remote/v1/device'] = (200, {'id': 'x'});
      final second = client(external: ext(outside), memory: memory);
      addTearDown(second.dispose);
      await second.checkDevice();
      expect(outside.seen, hasLength(2));
      expect(home.seen, isEmpty);
    });

    test('retries the other route when the remembered one fails', () async {
      final memory = _Memory(NetworkRoute.outside);
      final deadPort = outside.port;
      await outside.close();
      final remote = RemoteClient(
        endpoint: Endpoint(
          hosts: const ['127.0.0.1'],
          port: home.port,
          fingerprint: 'unused',
          external: ExternalAddress('localhost', deadPort),
        ),
        token: 'tok',
        scheme: 'http',
        routeMemory: memory,
      );
      addTearDown(remote.dispose);
      await remote.checkDevice();
      expect(home.seen, hasLength(1));
      expect(memory.value, NetworkRoute.home);
      outside = await FakeMac.start();
    });

    test('nothing answering on any route is unreachable', () async {
      final homePort = home.port;
      final outsidePort = outside.port;
      await home.close();
      await outside.close();
      final remote = client(external: ExternalAddress('localhost', outsidePort), homePort: homePort);
      addTearDown(remote.dispose);
      await expectLater(
        remote.checkDevice(),
        throwsA(isA<RemoteFailure>().having((f) => f.kind, 'kind', FailureKind.unreachable)),
      );
      home = await FakeMac.start();
      outside = await FakeMac.start();
    });

    test('the stream uses the same candidates', () async {
      final homePort = home.port;
      await home.close();
      outside.answers['GET /api/assistant/stream'] = (
        200,
        'event: snapshot\ndata: ${jsonEncode(snapshotJson([requestJson()]))}\n\n',
      );
      final remote = client(external: ext(outside), homePort: homePort);
      addTearDown(remote.dispose);
      final snapshots = await remote.snapshots().toList();
      expect(snapshots, hasLength(1));
      expect(remote.route.value, NetworkRoute.outside);
      home = await FakeMac.start();
    });
  });

  group('certificate pinning on every endpoint', () {
    late List<String> homeSeen;
    late List<String> outsideSeen;
    late HttpServer wrongHome;
    late HttpServer rightOutside;

    setUp(() async {
      homeSeen = [];
      outsideSeen = [];
      wrongHome = await _tlsServer(otherCertificatePem, otherPrivateKeyPem, homeSeen);
      rightOutside = await _tlsServer(testCertificatePem, testPrivateKeyPem, outsideSeen);
    });
    tearDown(() async {
      await wrongHome.close(force: true);
      await rightOutside.close(force: true);
    });

    RemoteClient pinned({required HttpServer homeServer, required HttpServer outsideServer, String? pin}) =>
        RemoteClient(
          endpoint: Endpoint(
            hosts: const ['127.0.0.1'],
            port: homeServer.port,
            fingerprint: pin ?? testCertificateFingerprint,
            external: ExternalAddress('localhost', outsideServer.port),
          ),
          token: 'tok',
        );

    Future<int> deadPort() async {
      final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      final port = server.port;
      await server.close();
      return port;
    }

    RemoteClient custom({required int homePort, ExternalAddress? external}) => RemoteClient(
      endpoint: Endpoint(
        hosts: const ['127.0.0.1'],
        port: homePort,
        fingerprint: testCertificateFingerprint,
        external: external,
      ),
      token: 'tok',
      homeConnectTimeout: const Duration(milliseconds: 500),
    );

    Matcher failsWith(FailureKind kind) => throwsA(isA<RemoteFailure>().having((f) => f.kind, 'kind', kind));

    test(
      'another device on the home address (other certificate) is skipped: connects over the external one',
      () async {
        final remote = pinned(homeServer: wrongHome, outsideServer: rightOutside);
        addTearDown(remote.dispose);
        expect((await remote.snapshot()).requests, hasLength(1));
        expect(remote.route.value, NetworkRoute.outside);
        // The foreign device never saw a request, so never saw the token.
        expect(homeSeen, isEmpty);
        expect(outsideSeen, ['/api/assistant']);
      },
    );

    test('the stream skips it the same way', () async {
      final remote = pinned(homeServer: wrongHome, outsideServer: rightOutside);
      addTearDown(remote.dispose);
      await remote.snapshots().toList();
      expect(homeSeen, isEmpty);
      expect(remote.route.value, NetworkRoute.outside);
    });

    test('a wrong certificate on the external address is a hard certificateMismatch', () async {
      final remote = custom(
        homePort: await deadPort(),
        external: ExternalAddress('localhost', wrongHome.port),
      );
      addTearDown(remote.dispose);
      await expectLater(remote.snapshot(), failsWith(FailureKind.certificateMismatch));
      expect(homeSeen, isEmpty);
    });

    test(
      'a wrong certificate on the external address stays a mismatch when the home network also fails',
      () async {
        final remote = pinned(homeServer: wrongHome, outsideServer: wrongHome);
        addTearDown(remote.dispose);
        await expectLater(remote.snapshot(), failsWith(FailureKind.certificateMismatch));
        await expectLater(remote.snapshots().toList(), failsWith(FailureKind.certificateMismatch));
      },
    );

    test('a wrong home certificate with the external address down is plain unreachable', () async {
      final remote = custom(
        homePort: wrongHome.port,
        external: ExternalAddress('localhost', await deadPort()),
      );
      addTearDown(remote.dispose);
      await expectLater(remote.snapshot(), failsWith(FailureKind.unreachable));
      await expectLater(remote.snapshots().toList(), failsWith(FailureKind.unreachable));
      expect(homeSeen, isEmpty);
    });

    test('a wrong home certificate and no other endpoint at all is a certificateMismatch', () async {
      final remote = custom(homePort: wrongHome.port);
      addTearDown(remote.dispose);
      await expectLater(remote.snapshot(), failsWith(FailureKind.certificateMismatch));
      await expectLater(remote.snapshots().toList(), failsWith(FailureKind.certificateMismatch));
    });

    test('the same pin accepts the right certificate on the external address', () async {
      final deadHome = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
      final deadPort = deadHome.port;
      await deadHome.close();
      final remote = RemoteClient(
        endpoint: Endpoint(
          hosts: const ['127.0.0.1'],
          port: deadPort,
          fingerprint: testCertificateFingerprint,
          external: ExternalAddress('localhost', rightOutside.port),
        ),
        token: 'tok',
        homeConnectTimeout: const Duration(milliseconds: 500),
      );
      addTearDown(remote.dispose);
      expect((await remote.snapshot()).requests, hasLength(1));
      expect(remote.route.value, NetworkRoute.outside);
    });
  });

  group('learning the external address from the Mac', () {
    Future<(RemoteClient, List<ExternalAddress?>)> connect(ExternalAddress? stored) async {
      final changed = <ExternalAddress?>[];
      final remote = client(external: stored, onChanged: (address) async => changed.add(address));
      addTearDown(remote.dispose);
      await remote.checkDevice();
      await remote.settle();
      return (remote, changed);
    }

    test('adopts a different valid address and reports it', () async {
      home.answers['GET /remote/v1/endpoint'] = (200, {'ext': 'new.example.org:9000'});
      final (remote, changed) = await connect(ExternalAddress.parse('old.example.org:8000'));
      expect(remote.endpoint.external.toString(), 'new.example.org:9000');
      expect(changed.single.toString(), 'new.example.org:9000');
      expect(home.endpointChecks.single.headers.value('authorization'), 'Bearer tok');
    });

    test('learns one when none was stored', () async {
      home.answers['GET /remote/v1/endpoint'] = (200, {'ext': '[2001:db8::1]:443'});
      final (remote, changed) = await connect(null);
      expect(remote.endpoint.external.toString(), '[2001:db8::1]:443');
      expect(changed, hasLength(1));
    });

    test('the same address changes nothing', () async {
      home.answers['GET /remote/v1/endpoint'] = (200, {'ext': 'old.example.org:8000'});
      final (_, changed) = await connect(ExternalAddress.parse('old.example.org:8000'));
      expect(changed, isEmpty);
    });

    for (final entry in <String, (int, Object?)>{
      'a malformed address': (200, {'ext': 'not an address'}),
      'a port out of range': (200, {'ext': 'new.example.org:70000'}),
      'an unbracketed IPv6 address': (200, {'ext': 'fd00::1:443'}),
      'a wrong type': (200, {'ext': 42}),
      'an answer that is not JSON': (200, 'hello'),
      'an old Mac that has no such route': (404, {'error': 'Not found.'}),
      'a refusal': (401, {'error': 'unauthorized'}),
      'a server error': (500, null),
    }.entries) {
      test('ignores ${entry.key}', () async {
        home.answers['GET /remote/v1/endpoint'] = entry.value;
        final (remote, changed) = await connect(ExternalAddress.parse('old.example.org:8000'));
        expect(remote.endpoint.external.toString(), 'old.example.org:8000');
        expect(changed, isEmpty);
      });
    }

    test(
      'an explicit null clears the stored address and reports it; an absent key changes nothing',
      () async {
        home.answers['GET /remote/v1/endpoint'] = (200, {'ext': null});
        final changed = <ExternalAddress?>[];
        final remote = client(
          external: ExternalAddress.parse('old.example.org:8000'),
          onChanged: (address) async => changed.add(address),
        );
        addTearDown(remote.dispose);
        await remote.checkDevice();
        await remote.settle();
        expect(remote.endpoint.external, isNull);
        expect(changed, [null]);

        home.answers['GET /remote/v1/endpoint'] = (200, <String, Object?>{});
        final kept = client(external: ExternalAddress.parse('old.example.org:8000'));
        addTearDown(kept.dispose);
        await kept.checkDevice();
        await kept.settle();
        expect(kept.endpoint.external.toString(), 'old.example.org:8000');
      },
    );

    test('asks once per connection, not on every request', () async {
      home.answers['GET /remote/v1/endpoint'] = (200, {'ext': 'old.example.org:8000'});
      final (remote, _) = await connect(ExternalAddress.parse('old.example.org:8000'));
      await remote.checkDevice();
      await remote.checkDevice();
      expect(home.endpointChecks, hasLength(1));
    });
  });
}
