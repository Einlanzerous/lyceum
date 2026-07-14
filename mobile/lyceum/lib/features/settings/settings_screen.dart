import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../auth/auth_controller.dart';
import '../../prefs/reading_font.dart';
import '../../prefs/theme_controller.dart';
import '../../theme/lyceum_colors.dart';
import '../../theme/lyceum_theme.dart';
import '../../widgets/segmented_control.dart';
import '../library/library_controller.dart';
import 'account_section.dart';
import 'server_settings.dart';

class SettingsScreen extends ConsumerWidget {
  const SettingsScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final lyc = context.lyc;
    final mode = ref.watch(themeControllerProvider);
    final font = ref.watch(readingFontProvider);

    return Scaffold(
      body: SafeArea(
        // See LibraryScreen: guard against the WebView reader zeroing the top
        // inset so this screen never slides under the status bar.
        minimum: EdgeInsets.only(top: MediaQuery.viewPaddingOf(context).top),
        child: ListView(
          padding: const EdgeInsets.fromLTRB(20, 16, 20, 48),
          children: [
            _BackPill(onTap: () => context.go('/')),
            const SizedBox(height: 24),
            Text('PREFERENCES',
                style: TextStyle(
                  fontSize: 11.5,
                  fontWeight: FontWeight.w700,
                  letterSpacing: 3,
                  color: lyc.brass,
                )),
            const SizedBox(height: 6),
            Text('Settings', style: Theme.of(context).textTheme.headlineLarge),
            const SizedBox(height: 28),

            // Account. Replaces the LYCM-700 local "Profile" label: the name now
            // lives on the server and travels with the person, not the handset.
            if (ref.watch(authControllerProvider).user != null) ...[
              const _Group(title: 'Account', child: AccountSection()),
              const SizedBox(height: 20),
            ],

            // Your devices — only where sessions exist to revoke. An auth-off
            // server has none.
            if (ref.watch(enforcedProvider)) ...[
              const _Group(title: 'Your devices', child: DevicesSection()),
              const SizedBox(height: 20),
            ],

            // Connection (always shown — Android always needs a server).
            _Group(
              title: 'Connection',
              child: ServerSettings(
                onSaved: () =>
                    ref.read(libraryControllerProvider.notifier).refresh(),
              ),
            ),
            const SizedBox(height: 20),

            // Appearance.
            _Group(
              title: 'Appearance',
              child: Row(
                mainAxisAlignment: MainAxisAlignment.spaceBetween,
                children: [
                  Text('Theme', style: TextStyle(fontSize: 14, color: lyc.reading)),
                  LycSegmentedControl<LyceumThemeMode>(
                    selected: mode,
                    onChanged: (m) =>
                        ref.read(themeControllerProvider.notifier).set(m),
                    options: const [
                      (value: LyceumThemeMode.dark, label: 'Dark'),
                      (value: LyceumThemeMode.light, label: 'Light'),
                    ],
                  ),
                ],
              ),
            ),
            const SizedBox(height: 20),

            // Reading.
            _Group(
              title: 'Reading',
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    mainAxisAlignment: MainAxisAlignment.spaceBetween,
                    children: [
                      Text('Font', style: TextStyle(fontSize: 14, color: lyc.reading)),
                      LycSegmentedControl<ReadingFont>(
                        selected: font,
                        onChanged: (f) =>
                            ref.read(readingFontProvider.notifier).set(f),
                        options: const [
                          (value: ReadingFont.publisher, label: 'Publisher'),
                          (value: ReadingFont.serif, label: 'Serif'),
                          (value: ReadingFont.sans, label: 'Sans'),
                        ],
                      ),
                    ],
                  ),
                  const SizedBox(height: 16),
                  Text(
                    'The quick brown fox jumps over the lazy dog.',
                    style: TextStyle(
                      fontFamily: switch (font) {
                        ReadingFont.serif => 'Georgia',
                        ReadingFont.sans => kBodyFont,
                        ReadingFont.publisher => kBodyFont,
                      },
                      fontSize: 16,
                      height: 1.5,
                      color: lyc.reading,
                    ),
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _Group extends StatelessWidget {
  const _Group({required this.title, required this.child});
  final String title;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(18),
      decoration: BoxDecoration(
        color: lyc.surface,
        borderRadius: BorderRadius.circular(LycRadii.card),
        border: Border.all(color: lyc.border),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            title,
            style: TextStyle(
              fontSize: 12,
              fontWeight: FontWeight.w700,
              letterSpacing: 0.5,
              color: lyc.muted,
            ),
          ),
          const SizedBox(height: 14),
          child,
        ],
      ),
    );
  }
}

class _BackPill extends StatelessWidget {
  const _BackPill({required this.onTap});
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    return GestureDetector(
      onTap: onTap,
      behavior: HitTestBehavior.opaque,
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 8),
        decoration: BoxDecoration(
          borderRadius: BorderRadius.circular(LycRadii.pill),
          border: Border.all(color: lyc.borderStrong),
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(Icons.arrow_back, size: 16, color: lyc.muted),
            const SizedBox(width: 6),
            Text('Library', style: TextStyle(fontSize: 13, color: lyc.text)),
          ],
        ),
      ),
    );
  }
}
