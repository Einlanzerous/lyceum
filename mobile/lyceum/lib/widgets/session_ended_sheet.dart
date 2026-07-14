import 'package:flutter/material.dart';

import '../theme/lyceum_colors.dart';
import 'lyc_sheet.dart';

/// The sheet a dead session raises, wherever it lands.
///
/// It appears for exactly one thing: a credential this device *was using* stopped
/// working. That covers an expired session, one revoked from another device, and
/// an account the owner removed — and the server tells us which of those it was,
/// never. So the copy names the causes it can and claims nothing it cannot check.
///
/// It deliberately does **not** appear for a device that simply isn't signed in.
/// Being asked to sign in is not the same as being signed out, and an app that
/// cannot tell the difference greets a fresh install with "your account was
/// removed and your reading positions were cleared".
Future<void> showSessionEndedSheet(BuildContext context) {
  return showLycSheet<void>(
    context: context,
    builder: (context) => const _SessionEndedSheet(),
  );
}

class _SessionEndedSheet extends StatelessWidget {
  const _SessionEndedSheet();

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;

    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            Icon(Icons.lock_outline_rounded, size: 20, color: lyc.muted),
            const SizedBox(width: 10),
            Expanded(
              child: Text(
                "You've been signed out.",
                style: Theme.of(context).textTheme.titleLarge,
              ),
            ),
          ],
        ),
        const SizedBox(height: 12),
        Text.rich(
          TextSpan(
            children: [
              TextSpan(
                text: 'Your place is saved.',
                style: TextStyle(fontWeight: FontWeight.w700, color: lyc.text),
              ),
              const TextSpan(
                text: ' Sign back in on this device to pick up exactly where you '
                    'left off — nothing was lost. This can happen if your session '
                    'expired, or was signed out from another device.',
              ),
            ],
          ),
          style: TextStyle(fontSize: 13.5, height: 1.5, color: lyc.muted),
        ),
        const SizedBox(height: 20),
        SizedBox(
          width: double.infinity,
          child: FilledButton(
            onPressed: () => Navigator.of(context).pop(),
            child: const Text('Sign in'),
          ),
        ),
      ],
    );
  }
}
