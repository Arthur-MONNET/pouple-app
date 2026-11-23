import 'package:flutter/material.dart';

/// Covers the app while the lock is on.
class LockScreen extends StatefulWidget {
  const LockScreen({super.key, required this.onUnlock});

  final Future<void> Function() onUnlock;

  @override
  State<LockScreen> createState() => _LockScreenState();
}

class _LockScreenState extends State<LockScreen> {
  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addPostFrameCallback((_) => widget.onUnlock());
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Icon(Icons.lock_outline, size: 48),
            const SizedBox(height: 16),
            const Text('Pouple is locked'),
            const SizedBox(height: 16),
            FilledButton(onPressed: widget.onUnlock, child: const Text('Unlock')),
          ],
        ),
      ),
    );
  }
}
