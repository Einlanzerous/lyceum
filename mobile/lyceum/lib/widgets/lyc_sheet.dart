import 'package:flutter/material.dart';

import '../theme/lyceum_colors.dart';

/// The app's one modal surface.
///
/// Android's idiom is a bottom sheet, not a centred dialog — it comes from the
/// thumb, and the back gesture dismisses it without anyone having to aim at an
/// ✕. Every account surface that the web renders as a modal is a sheet here.
///
/// [dismissible] is false for the sheets that are *destroying something* by
/// closing — the invite reveal, where a stray back-swipe would take the only
/// copy of an unrecoverable key with it.
Future<T?> showLycSheet<T>({
  required BuildContext context,
  required WidgetBuilder builder,
  bool dismissible = true,
}) {
  return showModalBottomSheet<T>(
    context: context,
    isScrollControlled: true,
    isDismissible: dismissible,
    enableDrag: dismissible,
    backgroundColor: Colors.transparent,
    barrierColor: context.lyc.isDark
        ? const Color(0xB8080807)
        : const Color(0x661C1A17),
    builder: (context) => _SheetShell(child: Builder(builder: builder)),
  );
}

class _SheetShell extends StatelessWidget {
  const _SheetShell({required this.child});
  final Widget child;

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    return Padding(
      // Lift above the keyboard: the invite field and the rename field are both
      // typed into from inside a sheet.
      padding: EdgeInsets.only(bottom: MediaQuery.viewInsetsOf(context).bottom),
      child: Container(
        width: double.infinity,
        decoration: BoxDecoration(
          color: lyc.surface,
          border: Border.all(color: lyc.border),
          borderRadius: const BorderRadius.vertical(
            top: Radius.circular(LycRadii.lg),
          ),
        ),
        child: SafeArea(
          top: false,
          child: SingleChildScrollView(
            child: Padding(
              padding: const EdgeInsets.fromLTRB(22, 12, 22, 22),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Center(
                    child: Container(
                      width: 36,
                      height: 4,
                      margin: const EdgeInsets.only(bottom: 18),
                      decoration: BoxDecoration(
                        color: lyc.borderStrong,
                        borderRadius: BorderRadius.circular(LycRadii.pill),
                      ),
                    ),
                  ),
                  child,
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}

/// A tinted callout — the shape the accounts copy leans on constantly: the
/// rejected-invite banner, the invite warning, the auth-off panel.
class LycNotice extends StatelessWidget {
  const LycNotice({
    super.key,
    required this.tone,
    required this.child,
    this.icon,
  });

  final LycTone tone;
  final Widget child;
  final IconData? icon;

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    final color = switch (tone) {
      LycTone.error => lyc.error,
      LycTone.warning => lyc.brass,
      LycTone.success => lyc.success,
      LycTone.neutral => lyc.muted,
    };
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.fromLTRB(15, 13, 15, 14),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.10),
        border: Border.all(color: color.withValues(alpha: 0.35)),
        borderRadius: BorderRadius.circular(LycRadii.card),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (icon != null) ...[
            Icon(icon, size: 17, color: color),
            const SizedBox(width: 11),
          ],
          Expanded(child: child),
        ],
      ),
    );
  }
}

enum LycTone { error, warning, success, neutral }
