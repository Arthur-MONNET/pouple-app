/// The ngrok static domain the Mac is reachable through: a bare DNS name, no scheme, port, path or IP literal.
library;

final _label = RegExp(r'^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$');

/// The normalized (lowercase) domain, or null when [text] is not a strict DNS name: at least two labels, letters,
/// digits and inner hyphens only, at most 253 characters, not an IP address. Surrounding whitespace is tolerated.
String? parseTunnelDomain(String? text) {
  if (text == null) return null;
  final value = text.trim().toLowerCase();
  if (value.isEmpty || value.length > 253) return null;
  final labels = value.split('.');
  if (labels.length < 2 || !labels.every(_label.hasMatch)) return null;
  // An all-numeric last label would make this look like an IPv4 address.
  if (RegExp(r'^[0-9]+$').hasMatch(labels.last)) return null;
  return value;
}
