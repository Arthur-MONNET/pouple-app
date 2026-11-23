import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../data/net/remote_client.dart';
import '../data/storage/credentials.dart';
import '../domain/app_lock.dart';
import '../domain/session.dart';
import '../notifications/attention_notifier.dart';
import '../notifications/background_check.dart';
import 'scoped_route.dart';

class SettingsScreen extends StatelessWidget {
  const SettingsScreen({super.key});

  Future<void> _unpair(BuildContext context) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        title: const Text('Unpair this phone?'),
        content: const Text('It will lose access to your conversations. You can pair it again from the Mac.'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(context, false), child: const Text('Cancel')),
          FilledButton(onPressed: () => Navigator.pop(context, true), child: const Text('Unpair')),
        ],
      ),
    );
    if (confirmed != true || !context.mounted) return;
    final session = context.read<SessionController>();
    final navigator = Navigator.of(context);
    await const BackgroundCheck().cancel();
    await AttentionNotifier().reset();
    await session.unpair();
    navigator.popUntil((route) => route.isFirst);
  }

  @override
  Widget build(BuildContext context) {
    final credentials = context.watch<SessionController>().credentials;
    final lock = context.watch<AppLock>();
    return Scaffold(
      appBar: AppBar(title: const Text('Settings')),
      body: ListView(
        children: [
          if (credentials != null) _Mac(credentials: credentials),
          if (credentials != null) _Connection(external: credentials.external),
          ListTile(
            leading: const Icon(Icons.qr_code_scanner),
            title: const Text("Update the Mac's address"),
            subtitle: const Text('Scan the address code the Mac shows when its Internet address changed.'),
            onTap: () => openAddressUpdate(context),
          ),
          const Divider(),
          SwitchListTile(
            title: const Text('App lock'),
            subtitle: const Text('Ask for your fingerprint or screen lock when the app opens.'),
            value: lock.enabled,
            onChanged: (value) async {
              final messenger = ScaffoldMessenger.of(context);
              final done = await lock.setEnabled(value);
              if (!done) {
                messenger.showSnackBar(const SnackBar(content: Text('The app lock could not be changed.')));
              }
            },
          ),
          ListTile(
            leading: const Icon(Icons.notifications_outlined),
            title: const Text('Notifications'),
            subtitle: const Text(
              'A short notice when a conversation needs you. It never shows what the conversation says. '
              'The phone checks every 15 minutes while the app is closed.',
            ),
            onTap: () => AttentionNotifier().requestPermission(),
          ),
          const Divider(),
          ListTile(
            leading: Icon(Icons.link_off, color: Theme.of(context).colorScheme.error),
            title: Text('Unpair this phone', style: TextStyle(color: Theme.of(context).colorScheme.error)),
            onTap: () => _unpair(context),
          ),
        ],
      ),
    );
  }
}

class _Mac extends StatelessWidget {
  const _Mac({required this.credentials});

  final Credentials credentials;

  @override
  Widget build(BuildContext context) {
    final fingerprint = credentials.fingerprint;
    return ListTile(
      leading: const Icon(Icons.laptop_mac),
      title: Text(credentials.macName),
      subtitle: Text(
        'Certificate ${fingerprint.substring(0, 16)}…\nHome network: ${credentials.hosts.join(', ')}:${credentials.port}',
      ),
      isThreeLine: true,
    );
  }
}

/// Which route the last connection took, and the external address when there is one.
class _Connection extends StatelessWidget {
  const _Connection({required this.external});

  final ExternalAddress? external;

  @override
  Widget build(BuildContext context) {
    final client = Provider.of<RemoteClient?>(context, listen: false);
    final externalLine = external == null ? 'No outside address set.' : 'Outside address: $external';
    Widget tile(NetworkRoute? route) {
      final (icon, title) = switch (route) {
        NetworkRoute.home => (Icons.home_outlined, 'Connected through your home network'),
        NetworkRoute.outside => (Icons.public, 'Connected from outside, through the Internet'),
        null => (Icons.sync_alt, 'Not connected yet'),
      };
      return ListTile(leading: Icon(icon), title: Text(title), subtitle: Text(externalLine));
    }

    if (client == null) return tile(null);
    return ValueListenableBuilder<NetworkRoute?>(
      valueListenable: client.route,
      builder: (context, route, _) => tile(route),
    );
  }
}
