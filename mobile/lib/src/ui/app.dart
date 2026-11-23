import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../data/stepup/step_up_signer.dart';
import '../domain/app_lock.dart';
import '../domain/session.dart';
import 'lock_screen.dart';
import 'paired_scope.dart';
import 'pairing_screen.dart';
import 'theme.dart';

/// The app: pairing until the phone is paired, then the conversations.
class PoupleApp extends StatelessWidget {
  const PoupleApp({
    super.key,
    required this.session,
    required this.lock,
    required this.signer,
    this.autoStart = true,
  });

  final SessionController session;
  final AppLock lock;
  final StepUpSigner signer;
  final bool autoStart;

  @override
  Widget build(BuildContext context) {
    return MultiProvider(
      providers: [
        ChangeNotifierProvider<SessionController>.value(value: session),
        ChangeNotifierProvider<AppLock>.value(value: lock),
        Provider<StepUpSigner>.value(value: signer),
      ],
      child: MaterialApp(
        title: 'Pouple',
        theme: poupleTheme(Brightness.light),
        darkTheme: poupleTheme(Brightness.dark),
        home: _Root(autoStart: autoStart),
      ),
    );
  }
}

class _Root extends StatelessWidget {
  const _Root({required this.autoStart});

  final bool autoStart;

  @override
  Widget build(BuildContext context) {
    final session = context.watch<SessionController>();
    final lock = context.watch<AppLock>();
    final credentials = session.credentials;
    if (session.phase == SessionPhase.loading) {
      return const Scaffold(body: Center(child: CircularProgressIndicator()));
    }
    if (session.phase == SessionPhase.unpaired || credentials == null) {
      return const PairingScreen();
    }
    if (lock.locked) return LockScreen(onUnlock: lock.unlock);
    return PairedScope(key: ValueKey(credentials.deviceId), credentials: credentials, autoStart: autoStart);
  }
}
