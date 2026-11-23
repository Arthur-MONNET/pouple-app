import 'package:pouple_companion/src/data/models/assistant.dart';

Map<String, Object?> requestJson({
  String id = 'a1b2c3d4e5f60718',
  String status = 'ready',
  String origin = 'in-app',
  String text = 'Fix the login redirect',
  String updatedAt = '2026-11-20T10:00:00.000Z',
  List<Object?>? questions,
  Object? proposal,
  Object? plan,
  List<Object?>? grants,
  List<Object?>? entries,
  String? error,
}) => {
  'id': id,
  'source': {
    'origin': origin,
    'channelId': 'in-app:$id',
    'channelName': null,
    'senderId': 'self',
    'senderName': '',
    'ts': '1',
    'permalink': null,
    'label': 'Fix the login redirect',
  },
  'text': text,
  'context': '',
  'receivedAt': '2026-11-20T09:00:00.000Z',
  'updatedAt': updatedAt,
  'status': status,
  'summary': null,
  'draftReply': null,
  'questions': questions ?? <Object?>[],
  'proposal': proposal,
  'plan': ?plan,
  'grants': ?grants,
  'entries': entries ?? <Object?>[],
  'error': error,
};

Map<String, Object?> snapshotJson(List<Object?> requests) => {
  'status': {'active': true, 'problem': null, 'lastPollAt': null, 'notices': <Object?>[]},
  'requests': requests,
  'topics': null,
  'alerts': <Object?>[],
};

Map<String, Object?> planJson({int actions = 1, String status = 'proposed', String id = 'plan0001'}) => {
  'id': id,
  'goal': 'Redirect after login',
  'brief': 'Edit the router',
  'actions': [
    for (var i = 0; i < actions; i++)
      {
        'kind': i == 0 ? 'code.edit' : 'gitlab.merge-request',
        'scope': {'service': 'code', 'id': 'web'},
        'summary': 'Action $i',
        'branch': 'fix/login',
        'base': 'main',
      },
  ],
  'status': status,
};

Map<String, Object?> proposalJson({String id = 'prop0001'}) => {
  'id': id,
  'title': 'Fix redirect',
  'description': 'Change the route guard',
  'projectId': 'web',
};

Map<String, Object?> questionJson({String id = 'q1', bool multiple = false}) => {
  'id': id,
  'text': 'Which environment?',
  'options': [
    {'id': 'a', 'label': 'Staging', 'description': null},
    {'id': 'b', 'label': 'Production', 'description': 'Careful'},
  ],
  if (multiple) 'multiple': true,
};

AssistantRequest parsedRequest(Map<String, Object?> json) =>
    AssistantSnapshot.fromJson(snapshotJson([json])).requests.single;
