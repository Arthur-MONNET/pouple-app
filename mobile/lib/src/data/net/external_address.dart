/// The address the router exposes to the Internet: a DNS name or IPv4 address, or a bracketed IPv6 literal, and a port.
library;

final _digitsAndDots = RegExp(r'^[0-9.]+$');
final _octet = RegExp(r'^(0|[1-9][0-9]{0,2})$');
final _label = RegExp(r'^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$');
final _port = RegExp(r'^[1-9][0-9]{0,4}$');

/// A validated `host:port`.
class ExternalAddress {
  const ExternalAddress(this.host, this.port);

  /// Reads `host:port`, `1.2.3.4:port` or `[ipv6]:port`. Anything else is a [FormatException] (whose message
  /// carries no part of the input).
  factory ExternalAddress.parse(String text) {
    final value = text.trim();
    final String host;
    final String portText;
    if (value.startsWith('[')) {
      final close = value.indexOf(']');
      if (close < 0 || close + 2 > value.length || value[close + 1] != ':') {
        throw const FormatException('Malformed external address.');
      }
      host = value.substring(1, close).toLowerCase();
      portText = value.substring(close + 2);
      try {
        Uri.parseIPv6Address(host);
      } on FormatException {
        throw const FormatException('Malformed external address.');
      }
    } else {
      final colon = value.lastIndexOf(':');
      if (colon <= 0) throw const FormatException('Malformed external address.');
      host = value.substring(0, colon).toLowerCase();
      portText = value.substring(colon + 1);
      if (!_validHostName(host)) throw const FormatException('Malformed external address.');
    }
    if (!_port.hasMatch(portText)) throw const FormatException('Malformed external address.');
    final port = int.parse(portText);
    if (port > 65535) throw const FormatException('Malformed external address.');
    return ExternalAddress(host, port);
  }

  /// Null instead of an exception.
  static ExternalAddress? tryParse(String? text) {
    if (text == null) return null;
    try {
      return ExternalAddress.parse(text);
    } on FormatException {
      return null;
    }
  }

  /// An IPv6 address has no brackets here.
  final String host;
  final int port;

  /// The form shown to people and stored: `host:port`, `[ipv6]:port`.
  @override
  String toString() => host.contains(':') ? '[$host]:$port' : '$host:$port';

  @override
  bool operator ==(Object other) => other is ExternalAddress && other.host == host && other.port == port;

  @override
  int get hashCode => Object.hash(host, port);
}

bool _validHostName(String host) {
  if (host.isEmpty || host.length > 253) return false;
  final labels = host.split('.');
  if (_digitsAndDots.hasMatch(host)) {
    return labels.length == 4 && labels.every((l) => _octet.hasMatch(l) && int.parse(l) <= 255);
  }
  return labels.every(_label.hasMatch);
}
