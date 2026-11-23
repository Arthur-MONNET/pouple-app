import 'package:flutter/material.dart';
import 'package:mobile_scanner/mobile_scanner.dart';
import 'package:provider/provider.dart';

import '../data/net/remote_failure.dart';
import '../data/pairing/address_payload.dart';
import '../data/pairing/pairing_payload.dart';
import '../data/stepup/step_up_signer.dart';
import '../domain/session.dart';

/// Pairs the phone: scan the QR code shown on the Mac, or paste its text.
class PairingScreen extends StatefulWidget {
  const PairingScreen({super.key});

  @override
  State<PairingScreen> createState() => _PairingScreenState();
}

class _PairingScreenState extends State<PairingScreen> {
  final _paste = TextEditingController();
  bool _scanning = false;
  bool _pairing = false;
  String? _error;

  @override
  void dispose() {
    _paste.dispose();
    super.dispose();
  }

  Future<void> _pair(String text) async {
    if (_pairing) return;
    if (payloadKind(text) == PayloadKind.address) {
      setState(() {
        _error = 'This is an address update code. Pair this phone with a pairing code first.';
        _scanning = false;
      });
      return;
    }
    final PairingPayload payload;
    try {
      payload = PairingPayload.parse(text);
    } on PairingPayloadException catch (problem) {
      setState(() {
        _error = problem.reason;
        _scanning = false;
      });
      return;
    }
    setState(() {
      _pairing = true;
      _scanning = false;
      _error = null;
    });
    final session = context.read<SessionController>();
    try {
      await session.pair(payload);
    } on RemoteFailure catch (failure) {
      if (mounted) setState(() => _error = failure.display);
    } on SignerException catch (problem) {
      if (mounted) setState(() => _error = problem.display);
    } finally {
      if (mounted) setState(() => _pairing = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final notice = context.watch<SessionController>().notice;
    final theme = Theme.of(context);
    if (_scanning) {
      return Scaffold(
        appBar: AppBar(
          title: const Text('Scan the QR code'),
          leading: IconButton(
            icon: const Icon(Icons.close),
            onPressed: () => setState(() => _scanning = false),
          ),
        ),
        body: MobileScanner(
          onDetect: (capture) {
            for (final barcode in capture.barcodes) {
              final value = barcode.rawValue;
              if (value != null && payloadKind(value) != PayloadKind.unknown) {
                _pair(value);
                return;
              }
            }
          },
          errorBuilder: (context, error) => const Center(
            child: Padding(
              padding: EdgeInsets.all(24),
              child: Text(
                'The camera is not available. Allow camera access, or paste the pairing text instead.',
              ),
            ),
          ),
        ),
      );
    }
    return Scaffold(
      appBar: AppBar(title: const Text('Pair with your Mac')),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          Text(
            'On your Mac, open Pouple settings, turn on remote access, choose the network and pair a phone. '
            'Pair while both devices are on the same network.',
            style: theme.textTheme.bodyLarge,
          ),
          if (notice != null) ...[const SizedBox(height: 16), _Message(text: notice, error: false)],
          const SizedBox(height: 24),
          FilledButton.icon(
            onPressed: _pairing ? null : () => setState(() => _scanning = true),
            icon: const Icon(Icons.qr_code_scanner),
            label: const Text('Scan the QR code'),
          ),
          const SizedBox(height: 32),
          Text('Or paste the pairing text', style: theme.textTheme.titleSmall),
          const SizedBox(height: 8),
          TextField(
            controller: _paste,
            enabled: !_pairing,
            minLines: 2,
            maxLines: 4,
            autocorrect: false,
            enableSuggestions: false,
            decoration: const InputDecoration(hintText: 'pouple-pair:v1?hosts=…'),
          ),
          const SizedBox(height: 8),
          OutlinedButton(onPressed: _pairing ? null : () => _pair(_paste.text), child: const Text('Pair')),
          if (_pairing) ...[
            const SizedBox(height: 24),
            const Center(child: CircularProgressIndicator()),
            const SizedBox(height: 8),
            const Center(child: Text('Pairing…')),
          ],
          if (_error != null) ...[const SizedBox(height: 16), _Message(text: _error!, error: true)],
        ],
      ),
    );
  }
}

class _Message extends StatelessWidget {
  const _Message({required this.text, required this.error});

  final String text;
  final bool error;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: error ? scheme.errorContainer : scheme.secondaryContainer,
        borderRadius: BorderRadius.circular(8),
      ),
      child: Text(
        text,
        style: TextStyle(color: error ? scheme.onErrorContainer : scheme.onSecondaryContainer),
      ),
    );
  }
}
