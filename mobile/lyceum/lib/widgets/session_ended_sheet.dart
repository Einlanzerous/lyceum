import 'package:flutter/material.dart';

import '../auth/auth_client.dart';
import '../theme/lyceum_colors.dart';
import '../theme/lyceum_theme.dart';
import 'lyc_sheet.dart';

/// The sheet a 401 raises, wherever it lands.
///
/// The two variants are not cosmetic. A session that stopped resolving and an
/// account that was deleted feel entirely different to the person holding the
/// phone, and only one of them means their reading positions are gone. Neither
/// message guesses: the server never says *why* a token stopped working, so both
/// are written to be true as stated.
Future<void> showSessionEndedSheet(BuildContext context, SessionEndReason reason) {
  return showLycSheet<void>(
    context: context,
    builder: (context) => _SessionEndedSheet(reason: reason),
  );
}

class _SessionEndedSheet extends StatelessWidget {
  const _SessionEndedSheet({required this.reason});
  final SessionEndReason reason;

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    final expired = reason == SessionEndReason.expired;

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
                expired
                    ? "You've been signed out."
                    : 'This device was signed out.',
                style: Theme.of(context).textTheme.titleLarge,
              ),
            ),
          ],
        ),
        const SizedBox(height: 12),
        if (expired)
          Text.rich(
            TextSpan(
              children: [
                TextSpan(
                  text: 'Your place is saved.',
                  style: TextStyle(
                    fontWeight: FontWeight.w700,
                    color: lyc.text,
                  ),
                ),
                const TextSpan(
                  text: ' Sign back in on this device to pick up exactly where '
                      'you left off — nothing was lost. This can happen if your '
                      'session expired, or was signed out from another device.',
                ),
              ],
            ),
            style: TextStyle(fontSize: 13.5, height: 1.5, color: lyc.muted),
          )
        else
          Text(
            'The library owner removed this account. Your reading positions were '
            'cleared, but the shared shelf is unaffected. To read again, ask the '
            'owner for a new invite.',
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

/// Shared by the sheets that show a name in a heading.
TextStyle sheetTitleStyle(BuildContext context) => TextStyle(
  fontFamily: kDisplayFont,
  fontWeight: FontWeight.w800,
  fontSize: 19,
  color: context.lyc.text,
);
