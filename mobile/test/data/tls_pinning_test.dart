import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:pouple_companion/src/data/net/remote_client.dart';
import 'package:pouple_companion/src/data/net/remote_failure.dart';

import '../support/fixtures.dart';
import '../support/test_certificate.dart';

void main() {
  late HttpServer server;

  setUp(() async {
    final context = SecurityContext()
      ..useCertificateChainBytes(utf8.encode(testCertificatePem))
      ..usePrivateKeyBytes(utf8.encode(testPrivateKeyPem));
    server = await HttpServer.bindSecure(InternetAddress.loopbackIPv4, 0, context);
    server.listen((request) async {
      if (request.uri.path == '/api/assistant/stream') {
        request.response.headers.contentType = ContentType('text', 'event-stream');
        request.response.write(': keep-alive\n\n');
        request.response.write(
          'event: snapshot\ndata: ${jsonEncode(snapshotJson([requestJson(status: 'awaiting-user')]))}\n\n',
        );
        await request.response.flush();
        await request.response.close();
        return;
      }
      request.response
        ..statusCode = 200
        ..write(jsonEncode(snapshotJson([requestJson()])));
      await request.response.close();
    });
  });

  tearDown(() => server.close(force: true));

  RemoteClient client(String fingerprint) => RemoteClient(
    endpoint: Endpoint(hosts: const ['127.0.0.1'], port: server.port, fingerprint: fingerprint),
    token: 'tok',
  );

  test('talks to the Mac whose certificate is pinned, with no CA and no host name check', () async {
    final remote = client(testCertificateFingerprint);
    addTearDown(remote.close);
    expect((await remote.snapshot()).requests, hasLength(1));
  });

  test('refuses a certificate that is not the pinned one', () async {
    final remote = client('0' * 64);
    addTearDown(remote.close);
    await expectLater(
      remote.snapshot(),
      throwsA(isA<RemoteFailure>().having((f) => f.kind, 'kind', FailureKind.certificateMismatch)),
    );
  });

  test('reads the snapshot stream over the pinned connection', () async {
    final remote = client(testCertificateFingerprint);
    addTearDown(remote.close);
    final snapshots = await remote.snapshots().toList();
    expect(snapshots, hasLength(1));
    expect(snapshots.single.requests.single.status.name, 'awaitingUser');
  });

  test('the stream refuses an unpinned certificate too', () async {
    final remote = client('f' * 64);
    addTearDown(remote.close);
    await expectLater(
      remote.snapshots().toList(),
      throwsA(isA<RemoteFailure>().having((f) => f.kind, 'kind', FailureKind.certificateMismatch)),
    );
  });
}
