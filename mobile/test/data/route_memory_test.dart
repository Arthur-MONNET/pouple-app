import 'package:flutter_test/flutter_test.dart';
import 'package:pouple_companion/src/data/net/route_memory.dart';
import 'package:shared_preferences/shared_preferences.dart';

void main() {
  test('remembers home, outside and tunnel, and ignores anything else', () async {
    SharedPreferences.setMockInitialValues({});
    const memory = PrefsRouteMemory();
    expect(await memory.read(), isNull);
    for (final route in NetworkRoute.values) {
      await memory.write(route);
      expect(await memory.read(), route);
    }
    expect(NetworkRoute.values.map((r) => r.name), ['home', 'outside', 'tunnel']);
    SharedPreferences.setMockInitialValues({'network.lastRoute': 'somewhere'});
    expect(await const PrefsRouteMemory().read(), isNull);
  });
}
