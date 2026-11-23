import 'package:flutter_test/flutter_test.dart';
import 'package:pouple_companion/src/notifications/attention_notifier.dart';

void main() {
  test('the notification text counts conversations and never carries content', () {
    expect(attentionText(1), 'A conversation needs you');
    expect(attentionText(3), '3 conversations need you');
  });
}
