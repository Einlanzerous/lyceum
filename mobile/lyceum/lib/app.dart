import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'auth/auth_controller.dart';
import 'prefs/theme_controller.dart';
import 'router/app_router.dart';
import 'theme/lyceum_colors.dart';
import 'theme/lyceum_theme.dart';
import 'widgets/brand_mark.dart';
import 'widgets/session_ended_sheet.dart';

class LyceumApp extends ConsumerWidget {
  const LyceumApp({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final mode = ref.watch(themeControllerProvider);
    final themeMode = mode == LyceumThemeMode.dark
        ? ThemeMode.dark
        : ThemeMode.light;

    // Ask the server who we are before painting anything that depends on the
    // answer. Without this the shelf would render, fire a dozen requests, and
    // collect a dozen 401s from a device that simply hadn't signed in yet.
    //
    // Only the *first* resolve is gated, and only while it is loading:
    //
    //  - A boot that fails (server off, wrong address) falls through to the app,
    //    where the library says so properly.
    //  - Re-asking after the server URL changes must NOT put the splash back up.
    //    Someone editing the address in Settings would be thrown out of the
    //    screen they were typing into and land back on the shelf.
    final booting =
        ref.watch(authBootstrapProvider).isLoading &&
        ref.watch(authControllerProvider).status == AuthStatus.unknown;

    if (booting) {
      return MaterialApp(
        title: 'Lyceum',
        debugShowCheckedModeBanner: false,
        theme: buildLyceumTheme(LyceumPalette.light),
        darkTheme: buildLyceumTheme(LyceumPalette.dark),
        themeMode: themeMode,
        home: const _Splash(),
      );
    }

    return MaterialApp.router(
      title: 'Lyceum',
      debugShowCheckedModeBanner: false,
      theme: buildLyceumTheme(LyceumPalette.light),
      darkTheme: buildLyceumTheme(LyceumPalette.dark),
      themeMode: themeMode,
      routerConfig: ref.watch(routerProvider),
      builder: (context, child) => _SessionGate(child: child ?? const SizedBox()),
    );
  }
}

/// Raises the "you've been signed out" sheet from anywhere in the app.
///
/// It lives above the router rather than on any one screen because the moment it
/// exists for is a 401 arriving *mid-chapter*: a calm sheet over the frozen page,
/// never a crash and never a lost place. The last position was already synced.
class _SessionGate extends ConsumerStatefulWidget {
  const _SessionGate({required this.child});
  final Widget child;

  @override
  ConsumerState<_SessionGate> createState() => _SessionGateState();
}

class _SessionGateState extends ConsumerState<_SessionGate> {
  bool _showing = false;

  @override
  Widget build(BuildContext context) {
    ref.listen(authControllerProvider, (prev, next) {
      if (!next.sessionEnded || _showing) return;

      // Not `context`: this widget sits above the router's Navigator, so its own
      // context has none in scope and showModalBottomSheet would throw — at the
      // one moment we most need it not to.
      final navContext = rootNavigatorKey.currentContext;
      if (navContext == null) return;

      _showing = true;
      showSessionEndedSheet(navContext).whenComplete(() {
        _showing = false;
        ref.read(authControllerProvider.notifier).clearEnded();
      });
    });
    return widget.child;
  }
}

class _Splash extends StatelessWidget {
  const _Splash();

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    return Scaffold(
      backgroundColor: lyc.bg,
      body: Center(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const BrandMark(),
            const SizedBox(height: 22),
            SizedBox(
              width: 22,
              height: 22,
              child: CircularProgressIndicator(strokeWidth: 2.4, color: lyc.brass),
            ),
          ],
        ),
      ),
    );
  }
}
