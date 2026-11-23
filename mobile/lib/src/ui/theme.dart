import 'package:flutter/material.dart';

const _accent = Color(0xFFF2668B);

ThemeData poupleTheme(Brightness brightness) {
  final scheme = ColorScheme.fromSeed(seedColor: _accent, brightness: brightness);
  return ThemeData(
    useMaterial3: true,
    colorScheme: scheme,
    appBarTheme: AppBarTheme(centerTitle: false, backgroundColor: scheme.surface, scrolledUnderElevation: 1),
    inputDecorationTheme: const InputDecorationTheme(border: OutlineInputBorder()),
  );
}
