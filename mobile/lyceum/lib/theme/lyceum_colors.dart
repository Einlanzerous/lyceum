import 'package:flutter/material.dart';

/// The Lyceum palette, ported 1:1 from the web app's CSS variables
/// (`web/src/styles/main.css`). Two instances — [dark] (brass-on-charcoal,
/// the default) and [light] (warm paper) — exposed as a [ThemeExtension] so
/// widgets can read the full token set via `Theme.of(context).extension`
/// (see the `context.lyc` helper at the bottom of this file).
@immutable
class LyceumPalette extends ThemeExtension<LyceumPalette> {
  const LyceumPalette({
    required this.brightness,
    required this.bg,
    required this.surface,
    required this.surfaceRaised,
    required this.panel,
    required this.text,
    required this.reading,
    required this.muted,
    required this.dim,
    required this.dimmer,
    required this.brass,
    required this.brassBright,
    required this.onBrass,
    required this.border,
    required this.borderStrong,
    required this.glass,
    required this.pillOnCover,
    required this.error,
    required this.success,
  });

  final Brightness brightness;
  final Color bg;
  final Color surface;
  final Color surfaceRaised;
  final Color panel;
  final Color text;
  final Color reading;
  final Color muted;
  final Color dim;
  final Color dimmer;
  final Color brass;
  final Color brassBright;
  final Color onBrass;
  final Color border;
  final Color borderStrong;
  final Color glass;
  final Color pillOnCover;
  final Color error;
  final Color success;

  bool get isDark => brightness == Brightness.dark;

  /// Faint brass wash used for ghost brass surfaces (toggle bg, active chips).
  Color get brassWash => brass.withValues(alpha: 0.12);
  Color get brassEdge => brass.withValues(alpha: 0.35);

  // --- DARK: brass-on-charcoal (default) ------------------------------------
  static const LyceumPalette dark = LyceumPalette(
    brightness: Brightness.dark,
    bg: Color(0xFF171717),
    surface: Color(0xFF1C1C1A),
    surfaceRaised: Color(0xFF201F1C),
    panel: Color(0xFF1A1A18),
    text: Color(0xFFEAEAE5),
    reading: Color(0xFFD8D6CF),
    muted: Color(0xFF9A9A92),
    dim: Color(0xFF7A7A72),
    dimmer: Color(0xFF6F6F68),
    brass: Color(0xFFC99A4E),
    brassBright: Color(0xFFDDB066),
    onBrass: Color(0xFF171717),
    border: Color(0x14EAEAE5), // rgba(234,234,229,0.08)
    borderStrong: Color(0x24EAEAE5), // rgba(234,234,229,0.14)
    glass: Color(0x80141413), // rgba(20,20,19,0.5)
    pillOnCover: Color(0xA80C0C0B), // rgba(12,12,11,0.66)
    error: Color(0xFFE08A6E),
    success: Color(0xFF5AA86A),
  );

  // --- LIGHT: warm paper ----------------------------------------------------
  static const LyceumPalette light = LyceumPalette(
    brightness: Brightness.light,
    bg: Color(0xFFF7F5F0),
    surface: Color(0xFFEFECE4),
    surfaceRaised: Color(0xFFFFFDF8),
    panel: Color(0xFFEFECE4),
    text: Color(0xFF1C1A17),
    reading: Color(0xFF2C2925),
    muted: Color(0xFF6B6660),
    dim: Color(0xFF908A80),
    dimmer: Color(0xFFA09A90),
    brass: Color(0xFF9C6F2E),
    brassBright: Color(0xFFB3853C),
    onBrass: Color(0xFFFFFDF8),
    border: Color(0x1A1C1A17), // rgba(28,26,23,0.1)
    borderStrong: Color(0x1F1C1A17), // rgba(28,26,23,0.12)
    glass: Color(0xB3FFFDF8), // rgba(255,253,248,0.7)
    pillOnCover: Color(0xEBFFFDF8), // rgba(255,253,248,0.92)
    error: Color(0xFFB4502F),
    success: Color(0xFF4F8A5E),
  );

  @override
  LyceumPalette copyWith({
    Brightness? brightness,
    Color? bg,
    Color? surface,
    Color? surfaceRaised,
    Color? panel,
    Color? text,
    Color? reading,
    Color? muted,
    Color? dim,
    Color? dimmer,
    Color? brass,
    Color? brassBright,
    Color? onBrass,
    Color? border,
    Color? borderStrong,
    Color? glass,
    Color? pillOnCover,
    Color? error,
    Color? success,
  }) {
    return LyceumPalette(
      brightness: brightness ?? this.brightness,
      bg: bg ?? this.bg,
      surface: surface ?? this.surface,
      surfaceRaised: surfaceRaised ?? this.surfaceRaised,
      panel: panel ?? this.panel,
      text: text ?? this.text,
      reading: reading ?? this.reading,
      muted: muted ?? this.muted,
      dim: dim ?? this.dim,
      dimmer: dimmer ?? this.dimmer,
      brass: brass ?? this.brass,
      brassBright: brassBright ?? this.brassBright,
      onBrass: onBrass ?? this.onBrass,
      border: border ?? this.border,
      borderStrong: borderStrong ?? this.borderStrong,
      glass: glass ?? this.glass,
      pillOnCover: pillOnCover ?? this.pillOnCover,
      error: error ?? this.error,
      success: success ?? this.success,
    );
  }

  @override
  LyceumPalette lerp(ThemeExtension<LyceumPalette>? other, double t) {
    if (other is! LyceumPalette) return this;
    return LyceumPalette(
      brightness: t < 0.5 ? brightness : other.brightness,
      bg: Color.lerp(bg, other.bg, t)!,
      surface: Color.lerp(surface, other.surface, t)!,
      surfaceRaised: Color.lerp(surfaceRaised, other.surfaceRaised, t)!,
      panel: Color.lerp(panel, other.panel, t)!,
      text: Color.lerp(text, other.text, t)!,
      reading: Color.lerp(reading, other.reading, t)!,
      muted: Color.lerp(muted, other.muted, t)!,
      dim: Color.lerp(dim, other.dim, t)!,
      dimmer: Color.lerp(dimmer, other.dimmer, t)!,
      brass: Color.lerp(brass, other.brass, t)!,
      brassBright: Color.lerp(brassBright, other.brassBright, t)!,
      onBrass: Color.lerp(onBrass, other.onBrass, t)!,
      border: Color.lerp(border, other.border, t)!,
      borderStrong: Color.lerp(borderStrong, other.borderStrong, t)!,
      glass: Color.lerp(glass, other.glass, t)!,
      pillOnCover: Color.lerp(pillOnCover, other.pillOnCover, t)!,
      error: Color.lerp(error, other.error, t)!,
      success: Color.lerp(success, other.success, t)!,
    );
  }
}

/// Corner-radius scale, mirroring the web `--*-r` tokens.
abstract final class LycRadii {
  static const double cover = 8; // book cover / thumbnail
  static const double card = 12; // cards / panels
  static const double lg = 16;
  static const double pill = 999; // buttons, chips, segmented controls
}

extension LyceumPaletteContext on BuildContext {
  /// The active Lyceum palette. Falls back to [LyceumPalette.dark] if the
  /// extension is somehow absent (should never happen once the theme is built).
  LyceumPalette get lyc =>
      Theme.of(this).extension<LyceumPalette>() ?? LyceumPalette.dark;
}
