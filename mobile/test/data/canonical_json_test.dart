import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:pouple_companion/src/data/stepup/canonical_json.dart';

void main() {
  const emptyBody = 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855';

  test('SHA-256 of an empty body is the well-known digest', () {
    expect(bodySha256(const []), emptyBody);
  });

  // The same vector as TestCanonicalJSONIsTheDocumentedOne in core/internal/remote/remote_test.go.
  test('matches the core vector for a plan approval', () {
    final canonical = canonicalStepUp(
      nonce: 'n1',
      deviceId: 'd1',
      route: StepUpRoute.plan,
      requestId: 'r1',
      planId: 'p1',
      proposalId: '',
      actions: [0, 2],
      bodySha256: 'ab',
    );
    expect(
      canonical,
      '{"v":1,"nonce":"n1","deviceId":"d1","route":"POST /api/assistant/requests/{id}/plan","requestId":"r1",'
      '"planId":"p1","proposalId":"","actions":[0,2],"bodySha256":"ab"}',
    );
  });

  // Produced by running StepUp.Canonical of core/internal/remote/stepup.go on these inputs.
  test('matches vectors produced by the Go verifier', () {
    expect(
      canonicalStepUp(
        nonce: '5d1e0aAz_9',
        deviceId: 'dev:1-x',
        route: StepUpRoute.proposal,
        requestId: 'a1b2.c3',
        planId: '',
        proposalId: 'prop0001',
        actions: [3, 1],
        bodySha256: emptyBody,
      ),
      '{"v":1,"nonce":"5d1e0aAz_9","deviceId":"dev:1-x","route":"POST /api/assistant/requests/{id}/proposal",'
      '"requestId":"a1b2.c3","planId":"","proposalId":"prop0001","actions":[3,1],"bodySha256":"$emptyBody"}',
    );
    expect(
      canonicalStepUp(
        nonce: 'n',
        deviceId: 'd',
        route: StepUpRoute.remove,
        requestId: 'r',
        planId: '',
        proposalId: '',
        actions: null,
        bodySha256: 'ab',
      ),
      '{"v":1,"nonce":"n","deviceId":"d","route":"DELETE /api/assistant/requests/{id}","requestId":"r",'
      '"planId":"","proposalId":"","actions":null,"bodySha256":"ab"}',
    );
  });

  test('matches the example of docs/REMOTE-API.md', () {
    const body = '{"decision":"approve","planId":"9f2c4b1a0d3e5f67","actions":[0,2]}';
    final canonical = canonicalStepUp(
      nonce: '5d1e',
      deviceId: 'dev',
      route: StepUpRoute.plan,
      requestId: 'a1b2c3d4e5f60718',
      planId: '9f2c4b1a0d3e5f67',
      proposalId: '',
      actions: [0, 2],
      bodySha256: bodySha256(utf8.encode(body)),
    );
    expect(
      canonical,
      startsWith(
        '{"v":1,"nonce":"5d1e","deviceId":"dev","route":"POST /api/assistant/requests/{id}/plan","requestId":"a1b2c3d4e5f60718","planId":"9f2c4b1a0d3e5f67","proposalId":"","actions":[0,2],"bodySha256":"',
      ),
    );
    expect(canonical, endsWith('"}'));
  });

  test('actions are null when the body has none, and [] when it is empty', () {
    String build(List<int>? actions) => canonicalStepUp(
      nonce: 'n',
      deviceId: 'd',
      route: StepUpRoute.proposal,
      requestId: 'r',
      planId: '',
      proposalId: 'prop',
      actions: actions,
      bodySha256: emptyBody,
    );
    expect(build(null), contains('"actions":null,'));
    expect(build(const []), contains('"actions":[],'));
  });

  test('a route without an id uses an empty requestId and keeps {id} as is for the others', () {
    final canonical = canonicalStepUp(
      nonce: 'n',
      deviceId: 'd',
      route: StepUpRoute.newConversation,
      requestId: '',
      planId: '',
      proposalId: '',
      actions: null,
      bodySha256: emptyBody,
    );
    expect(canonical, contains('"route":"POST /api/assistant/requests","requestId":""'));
  });

  test('delete route spelling', () {
    expect(StepUpRoute.remove, 'DELETE /api/assistant/requests/{id}');
  });

  test('a value that would need escaping is refused', () {
    expect(
      () => canonicalStepUp(
        nonce: 'n',
        deviceId: 'd',
        route: StepUpRoute.plan,
        requestId: 'r1","planId":"x',
        planId: '',
        proposalId: '',
        actions: null,
        bodySha256: 'ab',
      ),
      throwsA(isA<StepUpFormatException>()),
    );
    expect(
      () => canonicalStepUp(
        nonce: 'n',
        deviceId: 'd',
        route: 'POST /api/"x',
        requestId: 'r',
        planId: '',
        proposalId: '',
        actions: null,
        bodySha256: 'ab',
      ),
      throwsA(isA<StepUpFormatException>()),
    );
  });

  test('a value longer than 128 characters is refused', () {
    expect(
      () => canonicalStepUp(
        nonce: 'n' * 129,
        deviceId: 'd',
        route: StepUpRoute.plan,
        requestId: 'r',
        planId: '',
        proposalId: '',
        actions: null,
        bodySha256: 'ab',
      ),
      throwsA(isA<StepUpFormatException>()),
    );
  });
}
