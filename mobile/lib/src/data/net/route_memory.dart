import 'package:shared_preferences/shared_preferences.dart';

/// Which way the Mac was last reached: the home network, its external address, or the tunnel.
enum NetworkRoute { home, outside, tunnel }

/// Remembers the route that last worked, nothing else: no address, no name.
abstract interface class RouteMemory {
  Future<NetworkRoute?> read();
  Future<void> write(NetworkRoute route);
}

/// Kept in the app's preferences, which the background check shares.
class PrefsRouteMemory implements RouteMemory {
  const PrefsRouteMemory();

  static const _key = 'network.lastRoute';

  @override
  Future<NetworkRoute?> read() async {
    try {
      final value = (await SharedPreferences.getInstance()).getString(_key);
      for (final route in NetworkRoute.values) {
        if (route.name == value) return route;
      }
    } on Object {
      // Best effort.
    }
    return null;
  }

  @override
  Future<void> write(NetworkRoute route) async {
    try {
      await (await SharedPreferences.getInstance()).setString(_key, route.name);
    } on Object {
      // Best effort.
    }
  }
}
