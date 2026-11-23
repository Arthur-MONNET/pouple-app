import 'package:flutter/material.dart';
import 'package:provider/provider.dart';

import '../data/net/remote_client.dart';
import '../domain/conversation_actions.dart';
import '../domain/conversations_controller.dart';
import 'address_update_screen.dart';

/// A route that sees what the paired scope provides. The scope sits in the app's home, so a pushed route is not
/// below it unless it carries the objects along.
Route<T> scopedRoute<T>(BuildContext context, WidgetBuilder builder) {
  final controller = context.read<ConversationsController>();
  final actions = context.read<ConversationActions>();
  final client = Provider.of<RemoteClient?>(context, listen: false);
  return MaterialPageRoute<T>(
    builder: (_) => MultiProvider(
      providers: [
        ChangeNotifierProvider<ConversationsController>.value(value: controller),
        ChangeNotifierProvider<ConversationActions>.value(value: actions),
        if (client != null) Provider<RemoteClient>.value(value: client),
      ],
      child: Builder(builder: builder),
    ),
  );
}

/// Opens the screen that scans or takes the Mac's new address.
void openAddressUpdate(BuildContext context) {
  Navigator.of(context).push(MaterialPageRoute<void>(builder: (_) => const AddressUpdateScreen()));
}
