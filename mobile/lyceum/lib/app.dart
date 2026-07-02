import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'prefs/theme_controller.dart';
import 'router/app_router.dart';
import 'theme/lyceum_colors.dart';
import 'theme/lyceum_theme.dart';

class LyceumApp extends ConsumerWidget {
  const LyceumApp({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final mode = ref.watch(themeControllerProvider);
    final router = ref.watch(routerProvider);

    return MaterialApp.router(
      title: 'Lyceum',
      debugShowCheckedModeBanner: false,
      theme: buildLyceumTheme(LyceumPalette.light),
      darkTheme: buildLyceumTheme(LyceumPalette.dark),
      themeMode:
          mode == LyceumThemeMode.dark ? ThemeMode.dark : ThemeMode.light,
      routerConfig: router,
    );
  }
}
