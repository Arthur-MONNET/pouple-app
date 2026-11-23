import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../data/models/assistant.dart';
import '../data/net/remote_client.dart';
import '../data/net/remote_failure.dart';
import '../domain/conversation_actions.dart';
import 'conversation_screen.dart';

/// Starts a conversation with the assistant on the Mac.
class NewConversationScreen extends StatefulWidget {
  const NewConversationScreen({super.key});

  @override
  State<NewConversationScreen> createState() => _NewConversationScreenState();
}

class _NewConversationScreenState extends State<NewConversationScreen> {
  static const _maxLength = 4000;
  final _text = TextEditingController();
  List<ProjectInfo> _projects = const [];
  String? _project;

  @override
  void initState() {
    super.initState();
    _loadProjects();
  }

  @override
  void dispose() {
    _text.dispose();
    super.dispose();
  }

  Future<void> _loadProjects() async {
    try {
      final projects = await context.read<RemoteClient>().projects();
      if (mounted) setState(() => _projects = projects);
    } on RemoteFailure {
      // The projects are an optional hint; the conversation can start without one.
    }
  }

  Future<void> _start() async {
    final actions = context.read<ConversationActions>();
    final navigator = Navigator.of(context);
    final id = await actions.create(_text.text, projectId: _project);
    if (id != null && mounted) {
      navigator.pushReplacement(MaterialPageRoute<void>(builder: (_) => ConversationScreen(requestId: id)));
    }
  }

  @override
  Widget build(BuildContext context) {
    final actions = context.watch<ConversationActions>();
    return Scaffold(
      appBar: AppBar(title: const Text('New conversation')),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          TextField(
            controller: _text,
            minLines: 5,
            maxLines: 10,
            maxLength: _maxLength,
            autofocus: true,
            enabled: !actions.busy,
            textCapitalization: TextCapitalization.sentences,
            decoration: const InputDecoration(hintText: 'What do you need?'),
            onChanged: (_) => setState(() {}),
          ),
          if (_projects.isNotEmpty) ...[
            const SizedBox(height: 16),
            DropdownButtonFormField<String?>(
              initialValue: _project,
              decoration: const InputDecoration(labelText: 'Project (optional)'),
              items: [
                const DropdownMenuItem<String?>(child: Text('None')),
                for (final project in _projects)
                  DropdownMenuItem<String?>(value: project.id, child: Text(project.name)),
              ],
              onChanged: actions.busy ? null : (value) => setState(() => _project = value),
            ),
          ],
          if (actions.error != null) ...[
            const SizedBox(height: 12),
            Text(actions.error!, style: TextStyle(color: Theme.of(context).colorScheme.error)),
          ],
          const SizedBox(height: 16),
          FilledButton(
            onPressed: actions.busy || _text.text.trim().isEmpty ? null : _start,
            child: actions.busy
                ? const SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2))
                : const Text('Start'),
          ),
        ],
      ),
    );
  }
}
