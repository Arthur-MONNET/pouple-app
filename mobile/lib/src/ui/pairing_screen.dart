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
  bool _pasting = false;
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
    final scheme = theme.colorScheme;
    return Scaffold(
      body: SafeArea(
        child: CustomScrollView(
          slivers: [
            SliverFillRemaining(
              hasScrollBody: false,
              child: Padding(
                padding: const EdgeInsets.fromLTRB(24, 28, 24, 32),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    Text(
                      'Pouple',
                      style: TextStyle(fontWeight: FontWeight.w600, fontSize: 20, color: scheme.onSurface),
                    ),
                    const SizedBox(height: 32),
                    Semantics(
                      header: true,
                      child: Text(
                        'Connect to your Mac',
                        style: TextStyle(
                          fontWeight: FontWeight.w600,
                          fontSize: 30,
                          height: 1.1,
                          color: scheme.onSurface,
                        ),
                      ),
                    ),
                    const SizedBox(height: 8),
                    Text(
                      'On your Mac, open Settings → Phone → Add a device, then scan the code it shows. '
                      'Pair while both devices are on the same network.',
                      style: TextStyle(fontSize: 15, height: 1.5, color: scheme.onSurfaceVariant),
                    ),
                    if (notice != null) ...[const SizedBox(height: 16), _Message(text: notice, error: false)],
                    const SizedBox(height: 24),
                    Center(
                      child: _Viewfinder(onTap: _pairing ? null : () => setState(() => _scanning = true)),
                    ),
                    const SizedBox(height: 24),
                    OutlinedButton(
                      onPressed: _pairing ? null : () => setState(() => _pasting = !_pasting),
                      child: const Text('Paste the code instead'),
                    ),
                    if (_pasting) ...[
                      const SizedBox(height: 12),
                      TextField(
                        controller: _paste,
                        enabled: !_pairing,
                        minLines: 2,
                        maxLines: 4,
                        autocorrect: false,
                        enableSuggestions: false,
                        enableIMEPersonalizedLearning: false,
                        decoration: const InputDecoration(hintText: 'pouple-pair:v1?hosts=…'),
                      ),
                      const SizedBox(height: 8),
                      FilledButton(
                        onPressed: _pairing ? null : () => _pair(_paste.text),
                        child: const Text('Pair'),
                      ),
                    ],
                    if (_pairing) ...[
                      const SizedBox(height: 24),
                      const Center(child: CircularProgressIndicator()),
                      const SizedBox(height: 8),
                      const Center(child: Text('Pairing…')),
                    ],
                    if (_error != null) ...[const SizedBox(height: 16), _Message(text: _error!, error: true)],
                    const SizedBox(height: 20),
                    Row(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Padding(
                          padding: const EdgeInsets.only(top: 2),
                          child: Icon(Icons.lock_outline, size: 16, color: scheme.tertiary),
                        ),
                        const SizedBox(width: 10),
                        Expanded(
                          child: Text(
                            'Once paired, it works at home and away. Everything is encrypted between your phone '
                            'and your Mac.',
                            style: TextStyle(fontSize: 12.5, height: 1.5, color: scheme.onSurfaceVariant),
                          ),
                        ),
                      ],
                    ),
                  ],
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// The camera frame with four coral corner brackets. Tapping it opens the scanner.
class _Viewfinder extends StatelessWidget {
  const _Viewfinder({required this.onTap});

  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final scheme = Theme.of(context).colorScheme;
    Widget corner({required bool top, required bool left}) {
      final side = BorderSide(color: scheme.primary, width: 3);
      const round = Radius.circular(10);
      return Positioned(
        top: top ? 18 : null,
        bottom: top ? null : 18,
        left: left ? 18 : null,
        right: left ? null : 18,
        child: ExcludeSemantics(
          child: Container(
            width: 34,
            height: 34,
            decoration: BoxDecoration(
              border: Border(
                top: top ? side : BorderSide.none,
                bottom: top ? BorderSide.none : side,
                left: left ? side : BorderSide.none,
                right: left ? BorderSide.none : side,
              ),
              borderRadius: BorderRadius.only(
                topLeft: top && left ? round : Radius.zero,
                topRight: top && !left ? round : Radius.zero,
                bottomLeft: !top && left ? round : Radius.zero,
                bottomRight: !top && !left ? round : Radius.zero,
              ),
            ),
          ),
        ),
      );
    }

    return Semantics(
      button: true,
      label: 'Scan the QR code',
      child: Material(
        color: scheme.surfaceContainerLow,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(24),
          side: BorderSide(color: scheme.outlineVariant),
        ),
        child: InkWell(
          key: const Key('scan-viewfinder'),
          borderRadius: BorderRadius.circular(24),
          onTap: onTap,
          child: SizedBox(
            width: 272,
            height: 272,
            child: Stack(
              alignment: Alignment.center,
              children: [
                corner(top: true, left: true),
                corner(top: true, left: false),
                corner(top: false, left: true),
                corner(top: false, left: false),
                ExcludeSemantics(
                  child: Column(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Icon(Icons.qr_code_scanner, color: scheme.onSurfaceVariant),
                      const SizedBox(height: 8),
                      Text(
                        'Point the camera at the QR code',
                        style: TextStyle(fontSize: 13, color: scheme.onSurfaceVariant),
                      ),
                      const SizedBox(height: 2),
                      Text('Tap to scan', style: TextStyle(fontSize: 12, color: scheme.outline)),
                    ],
                  ),
                ),
              ],
            ),
          ),
        ),
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
        borderRadius: BorderRadius.circular(14),
      ),
      child: Text(
        text,
        style: TextStyle(color: error ? scheme.onErrorContainer : scheme.onSecondaryContainer),
      ),
    );
  }
}
