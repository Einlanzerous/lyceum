import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'prefs.dart';

/// Which palette is active. Mirrors the web `data-theme` attribute
/// (persisted under `lyceum.theme`, default **dark**).
enum LyceumThemeMode {
  dark,
  light;

  Brightness get brightness =>
      this == LyceumThemeMode.dark ? Brightness.dark : Brightness.light;
}

const _kThemeKey = 'lyceum.theme';

class ThemeController extends Notifier<LyceumThemeMode> {
  @override
  LyceumThemeMode build() {
    final raw = ref.watch(prefsProvider).getString(_kThemeKey);
    return raw == 'light' ? LyceumThemeMode.light : LyceumThemeMode.dark;
  }

  Future<void> set(LyceumThemeMode mode) async {
    await ref.read(prefsProvider).setString(_kThemeKey, mode.name);
    state = mode;
  }

  Future<void> toggle() => set(
        state == LyceumThemeMode.dark
            ? LyceumThemeMode.light
            : LyceumThemeMode.dark,
      );
}

final themeControllerProvider =
    NotifierProvider<ThemeController, LyceumThemeMode>(ThemeController.new);
