import 'package:flutter/material.dart';

import 'src/data/stepup/step_up_signer.dart';
import 'src/data/storage/credentials.dart';
import 'src/domain/app_lock.dart';
import 'src/domain/session.dart';
import 'src/ui/app.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  const signer = PlatformStepUpSigner();
  final session = SessionController(store: SecureCredentialStore(), signer: signer);
  final lock = AppLock(signer: signer);
  await lock.restore();
  await session.restore();
  runApp(PoupleApp(session: session, lock: lock, signer: signer));
}
