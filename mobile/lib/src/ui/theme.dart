import 'package:flutter/material.dart';

const _accent = Color(0xFFF2668B);

/// The status colors of the design that a color scheme has no slot for.
const poupleGreen = Color(0xFF4CC38A);
const poupleLavender = Color(0xFFC9C6FF);
const _idle = Color(0xFF3A3F48);

/// Dot color of a conversation that is working, ready or finished.
Color statusDotColor({required bool working, required bool ready}) =>
    working ? poupleLavender : (ready ? poupleGreen : _idle);

/// The design's tokens in dark mode (#0e0f12 page, #15171b cards, #2e323a borders, coral accent); the light theme
/// keeps the scheme derived from the accent. The fonts are the system ones: no font is bundled.
ThemeData poupleTheme(Brightness brightness) {
  var scheme = ColorScheme.fromSeed(seedColor: _accent, brightness: brightness);
  if (brightness == Brightness.dark) {
    scheme = scheme.copyWith(
      surface: const Color(0xFF0E0F12),
      surfaceContainerLowest: const Color(0xFF0E0F12),
      surfaceContainerLow: const Color(0xFF15171B),
      surfaceContainer: const Color(0xFF15171B),
      surfaceContainerHigh: const Color(0xFF1B1E24),
      surfaceContainerHighest: const Color(0xFF23262D),
      outline: const Color(0xFF3A3F48),
      outlineVariant: const Color(0xFF2E323A),
      primary: _accent,
      onPrimary: const Color(0xFF0E0F12),
      tertiary: poupleLavender,
      onSurface: const Color(0xFFECEDEF),
      onSurfaceVariant: const Color(0xFFA4A9B3),
    );
  }
  final shape = RoundedRectangleBorder(borderRadius: BorderRadius.circular(14));
  const size = Size(64, 48);
  return ThemeData(
    useMaterial3: true,
    colorScheme: scheme,
    scaffoldBackgroundColor: scheme.surface,
    appBarTheme: AppBarTheme(centerTitle: false, backgroundColor: scheme.surface, scrolledUnderElevation: 1),
    inputDecorationTheme: InputDecorationTheme(
      border: OutlineInputBorder(borderRadius: BorderRadius.circular(14)),
    ),
    filledButtonTheme: FilledButtonThemeData(
      style: FilledButton.styleFrom(
        minimumSize: size,
        shape: shape,
        textStyle: const TextStyle(fontWeight: FontWeight.w600, fontSize: 15),
      ),
    ),
    outlinedButtonTheme: OutlinedButtonThemeData(
      style: OutlinedButton.styleFrom(
        minimumSize: size,
        shape: shape,
        side: BorderSide(color: scheme.outline),
        textStyle: const TextStyle(fontWeight: FontWeight.w500, fontSize: 15),
      ),
    ),
    textButtonTheme: TextButtonThemeData(style: TextButton.styleFrom(minimumSize: const Size(64, 44))),
  );
}

/// A small uppercase section label.
TextStyle sectionLabelStyle(BuildContext context) => TextStyle(
  fontSize: 11,
  fontWeight: FontWeight.w500,
  letterSpacing: 0.88,
  color: Theme.of(context).colorScheme.onSurfaceVariant,
);

/// A rounded card on the page background.
BoxDecoration cardDecoration(BuildContext context, {double radius = 16}) {
  final scheme = Theme.of(context).colorScheme;
  return BoxDecoration(
    color: scheme.surfaceContainerLow,
    border: Border.all(color: scheme.outlineVariant),
    borderRadius: BorderRadius.circular(radius),
  );
}
