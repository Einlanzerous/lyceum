import 'package:flutter/material.dart';

import 'lyceum_colors.dart';

/// Display face (headings, wordmark, numerals) — matches the web `--font-display`.
const String kDisplayFont = 'Archivo';

/// UI/body face — matches the web `--font-ui`.
const String kBodyFont = 'HankenGrotesk';

/// Builds a Material 3 [ThemeData] from a [LyceumPalette]. The palette itself is
/// registered as a [ThemeExtension] so widgets can reach the full token set via
/// `context.lyc`.
ThemeData buildLyceumTheme(LyceumPalette p) {
  final scheme = ColorScheme(
    brightness: p.brightness,
    primary: p.brass,
    onPrimary: p.onBrass,
    primaryContainer: p.brassWash,
    onPrimaryContainer: p.brassBright,
    secondary: p.brass,
    onSecondary: p.onBrass,
    surface: p.surface,
    onSurface: p.text,
    surfaceContainerHighest: p.surfaceRaised,
    surfaceContainerHigh: p.surfaceRaised,
    surfaceContainer: p.panel,
    surfaceContainerLow: p.panel,
    surfaceContainerLowest: p.bg,
    onSurfaceVariant: p.muted,
    outline: p.borderStrong,
    outlineVariant: p.border,
    error: p.error,
    onError: p.onBrass,
  );

  final base = ThemeData(
    useMaterial3: true,
    brightness: p.brightness,
    colorScheme: scheme,
    scaffoldBackgroundColor: p.bg,
    fontFamily: kBodyFont,
    splashFactory: InkSparkle.splashFactory,
    extensions: [p],
  );

  TextStyle display(double size, FontWeight w, {Color? color, double? spacing}) =>
      TextStyle(
        fontFamily: kDisplayFont,
        fontSize: size,
        fontWeight: w,
        height: 1.1,
        letterSpacing: spacing,
        color: color ?? p.text,
      );

  TextStyle body(double size, FontWeight w, {Color? color, double? spacing}) =>
      TextStyle(
        fontFamily: kBodyFont,
        fontSize: size,
        fontWeight: w,
        letterSpacing: spacing,
        color: color ?? p.text,
      );

  return base.copyWith(
    textTheme: base.textTheme.copyWith(
      displaySmall: display(34, FontWeight.w800),
      headlineLarge: display(30, FontWeight.w800),
      headlineMedium: display(26, FontWeight.w700),
      headlineSmall: display(22, FontWeight.w700),
      titleLarge: display(20, FontWeight.w700),
      titleMedium: body(15, FontWeight.w700),
      titleSmall: body(13, FontWeight.w700),
      bodyLarge: body(16, FontWeight.w400, color: p.reading),
      bodyMedium: body(14, FontWeight.w400, color: p.text),
      bodySmall: body(12.5, FontWeight.w400, color: p.muted),
      labelLarge: body(14, FontWeight.w600),
      labelMedium: body(12, FontWeight.w600, color: p.muted),
      labelSmall: body(11, FontWeight.w600, color: p.dim),
    ),
    appBarTheme: AppBarTheme(
      backgroundColor: Colors.transparent,
      surfaceTintColor: Colors.transparent,
      elevation: 0,
      scrolledUnderElevation: 0,
      foregroundColor: p.text,
      centerTitle: false,
    ),
    cardTheme: CardThemeData(
      color: p.surface,
      surfaceTintColor: Colors.transparent,
      elevation: 0,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(LycRadii.card),
        side: BorderSide(color: p.border),
      ),
      margin: EdgeInsets.zero,
    ),
    dividerTheme: DividerThemeData(color: p.border, thickness: 1, space: 1),
    iconTheme: IconThemeData(color: p.muted),
    scrollbarTheme: ScrollbarThemeData(
      thumbColor: WidgetStatePropertyAll(p.borderStrong),
    ),
    inputDecorationTheme: InputDecorationTheme(
      filled: true,
      fillColor: p.bg,
      hintStyle: body(14, FontWeight.w400, color: p.dim),
      contentPadding: const EdgeInsets.symmetric(horizontal: 14, vertical: 13),
      border: OutlineInputBorder(
        borderRadius: BorderRadius.circular(10),
        borderSide: BorderSide(color: p.borderStrong),
      ),
      enabledBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(10),
        borderSide: BorderSide(color: p.borderStrong),
      ),
      focusedBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(10),
        borderSide: BorderSide(color: p.brass, width: 1.5),
      ),
    ),
    filledButtonTheme: FilledButtonThemeData(
      style: FilledButton.styleFrom(
        backgroundColor: p.brass,
        foregroundColor: p.onBrass,
        textStyle: body(14, FontWeight.w700),
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(LycRadii.pill),
        ),
        padding: const EdgeInsets.symmetric(horizontal: 18, vertical: 12),
      ),
    ),
    outlinedButtonTheme: OutlinedButtonThemeData(
      style: OutlinedButton.styleFrom(
        foregroundColor: p.text,
        textStyle: body(14, FontWeight.w600),
        side: BorderSide(color: p.borderStrong),
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(LycRadii.pill),
        ),
        padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 11),
      ),
    ),
    textButtonTheme: TextButtonThemeData(
      style: TextButton.styleFrom(
        foregroundColor: p.brass,
        textStyle: body(14, FontWeight.w600),
      ),
    ),
    snackBarTheme: SnackBarThemeData(
      backgroundColor: p.surfaceRaised,
      contentTextStyle: body(13.5, FontWeight.w500, color: p.text),
      actionTextColor: p.brass,
      behavior: SnackBarBehavior.floating,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(LycRadii.card),
        side: BorderSide(color: p.border),
      ),
    ),
  );
}
