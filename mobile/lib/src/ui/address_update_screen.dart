import 'package:flutter/material.dart';
import 'package:mobile_scanner/mobile_scanner.dart';
import 'package:provider/provider.dart';

import '../data/pairing/address_payload.dart';
import '../data/pairing/pairing_payload.dart';
import '../domain/session.dart';

/// Updates where the paired Mac is reached from outside: scan the address code the Mac shows, or paste its text.
class AddressUpdateScreen extends StatefulWidget {
  const AddressUpdateScreen({super.key});

  @override
  State<AddressUpdateScreen> createState() => _AddressUpdateScreenState();
}

class _AddressUpdateScreenState extends State<AddressUpdateScreen> {
  final _paste = TextEditingController();
  bool _scanning = false;
  bool _busy = false;
  String? _error;

  @override
  void dispose() {
    _paste.dispose();
    super.dispose();
  }

  Future<void> _apply(String text) async {
    if (_busy) return;
    final session = context.read<SessionController>();
    final messenger = ScaffoldMessenger.of(context);
    final navigator = Navigator.of(context);
    setState(() {
      _scanning = false;
      _busy = true;
      _error = null;
    });
    try {
      switch (payloadKind(text)) {
        case PayloadKind.address:
          final payload = AddressPayload.parse(text);
          await session.applyAddress(payload);
          final tunnel = payload.tunnel;
          messenger.showSnackBar(
            SnackBar(
              content: Text(
                tunnel == null
                    ? 'Address updated to ${payload.external}.'
                    : 'Address updated to ${payload.external}, tunnel $tunnel.',
              ),
            ),
          );
          navigator.pop();
        case PayloadKind.pairing:
          setState(
            () => _error = 'This is a pairing code. Here, scan the address code the Mac shows instead.',
          );
        case PayloadKind.unknown:
          setState(() => _error = 'This is not a Pouple address code.');
      }
    } on PairingPayloadException catch (problem) {
      if (mounted) setState(() => _error = problem.reason);
    } on AddressUpdateException catch (problem) {
      if (mounted) setState(() => _error = problem.reason);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    if (_scanning) {
      return Scaffold(
        appBar: AppBar(
          title: const Text('Scan the address code'),
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
                _apply(value);
                return;
              }
            }
          },
          errorBuilder: (context, error) => const Center(
            child: Padding(
              padding: EdgeInsets.all(24),
              child: Text(
                'The camera is not available. Allow camera access, or paste the address text instead.',
              ),
            ),
          ),
        ),
      );
    }
    return Scaffold(
      appBar: AppBar(title: const Text("Update the Mac's address")),
      body: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          Text(
            'If your Mac cannot be reached from outside your home, its Internet address may have changed. '
            'On the Mac, open Pouple settings and show the address code, then scan it here. '
            'It only updates the address: your pairing stays as it is.',
            style: theme.textTheme.bodyLarge,
          ),
          const SizedBox(height: 24),
          FilledButton.icon(
            onPressed: _busy ? null : () => setState(() => _scanning = true),
            icon: const Icon(Icons.qr_code_scanner),
            label: const Text('Scan the address code'),
          ),
          const SizedBox(height: 32),
          Text('Or paste the address text', style: theme.textTheme.titleSmall),
          const SizedBox(height: 8),
          TextField(
            controller: _paste,
            enabled: !_busy,
            minLines: 2,
            maxLines: 4,
            autocorrect: false,
            enableSuggestions: false,
            enableIMEPersonalizedLearning: false,
            decoration: const InputDecoration(hintText: 'pouple-address:v1?ext=…'),
          ),
          const SizedBox(height: 8),
          OutlinedButton(onPressed: _busy ? null : () => _apply(_paste.text), child: const Text('Update')),
          if (_error != null) ...[
            const SizedBox(height: 16),
            Container(
              padding: const EdgeInsets.all(12),
              decoration: BoxDecoration(
                color: theme.colorScheme.errorContainer,
                borderRadius: BorderRadius.circular(8),
              ),
              child: Text(_error!, style: TextStyle(color: theme.colorScheme.onErrorContainer)),
            ),
          ],
        ],
      ),
    );
  }
}
