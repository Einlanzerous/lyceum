import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../api/server_store.dart';
import '../../auth/invite_token.dart';
import '../../auth/onboarding.dart';
import '../../auth/session_store.dart';
import '../../theme/lyceum_colors.dart';
import 'scan_invite_screen.dart';

/// The scan-to-onboard handoff, from camera to signed in (LYCM-103).
///
/// Two screens start it — the front door, and the Library's connect prompt,
/// which is what a fresh install with no address actually lands on — and each
/// renders the outcome in its own voice. So the shared part stops at the point
/// where there is something to say: it scans, asks before moving a device that
/// is already somewhere, runs [Onboarder.connect], and hands back the result.
///
/// Returns null when the person backed out of the scanner or declined the move —
/// nothing happened, and nothing needs saying about it. [onScanned] fires only
/// once the invite is actually going to be used.
Future<OnboardResult?> scanAndOnboard(
  BuildContext context,
  WidgetRef ref, {
  String deviceLabel = '',
  ValueChanged<ScannedInvite>? onScanned,
}) async {
  final invite = await Navigator.of(context).push<ScannedInvite>(
    MaterialPageRoute(builder: (_) => const ScanInviteScreen()),
  );
  if (invite == null || !context.mounted) return null;

  final origin = invite.origin;
  final current = ref.read(serverUrlProvider);
  if (origin != null && current.isNotEmpty && origin != current) {
    final move = await _confirmMove(
      context,
      from: current,
      to: origin,
      signedIn: ref.read(sessionTokenProvider).isNotEmpty,
    );
    if (!move) return null;
  }

  // After the last chance to back out, and before anything can fail: a key the
  // server is about to refuse should still be sitting in the field the person
  // is looking at, and a key for a library they declined should not be.
  onScanned?.call(invite);

  return ref.read(onboarderProvider).connect(invite, deviceLabel: deviceLabel);
}

/// Ask before pointing a device that already has a library at a different one.
///
/// Silently would be wrong. The address change drops the session by design
/// (`ServerUrlController.set`), so an owner who scans a guest's invite out of
/// curiosity would find themselves signed out of their own shelf with no idea
/// what they touched. Naming both addresses makes the trade legible; an invite
/// for the library they are already on never gets here.
Future<bool> _confirmMove(
  BuildContext context, {
  required String from,
  required String to,
  required bool signedIn,
}) async {
  final lyc = context.lyc;
  final answer = await showDialog<bool>(
    context: context,
    builder: (dialogContext) => AlertDialog(
      title: const Text('Connect to a different library?'),
      content: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            'This invite is for a different address than the one this device '
            'is using.',
            style: TextStyle(fontSize: 13.5, height: 1.5, color: lyc.muted),
          ),
          const SizedBox(height: 14),
          _Address(label: 'NOW', value: from),
          const SizedBox(height: 8),
          _Address(label: 'INVITE', value: to),
          if (signedIn) ...[
            const SizedBox(height: 14),
            Text(
              'Connecting signs this device out of the library it is on now. '
              'Your books and your places stay on that server — you can come '
              'back with a fresh invite.',
              style: TextStyle(fontSize: 12.5, height: 1.5, color: lyc.muted),
            ),
          ],
        ],
      ),
      actions: [
        TextButton(
          onPressed: () => Navigator.of(dialogContext).pop(false),
          child: const Text('Cancel'),
        ),
        FilledButton(
          onPressed: () => Navigator.of(dialogContext).pop(true),
          child: const Text('Connect'),
        ),
      ],
    ),
  );
  return answer ?? false;
}

/// The scan affordance itself, so both front doors offer the same one thing in
/// the same shape — a primary action, not an icon tucked beside a text field.
class ScanInviteButton extends StatelessWidget {
  const ScanInviteButton({
    super.key,
    required this.onPressed,
    required this.busy,
    this.label = 'Scan invite',
  });

  final VoidCallback? onPressed;
  final bool busy;
  final String label;

  @override
  Widget build(BuildContext context) {
    if (busy) {
      return FilledButton(
        onPressed: null,
        child: Row(
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            SizedBox(
              width: 14,
              height: 14,
              child: CircularProgressIndicator(
                strokeWidth: 2,
                color: context.lyc.onBrass,
              ),
            ),
            const SizedBox(width: 9),
            const Text('Connecting…'),
          ],
        ),
      );
    }
    return FilledButton.icon(
      onPressed: onPressed,
      icon: const Icon(Icons.qr_code_scanner_rounded, size: 18),
      label: Text(label),
    );
  }
}

class _Address extends StatelessWidget {
  const _Address({required this.label, required this.value});
  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          label,
          style: TextStyle(
            fontSize: 9.5,
            fontWeight: FontWeight.w700,
            letterSpacing: 1.3,
            color: lyc.dim,
          ),
        ),
        const SizedBox(height: 2),
        Text(
          value,
          style: TextStyle(
            fontFamily: 'monospace',
            fontSize: 12.5,
            color: lyc.text,
          ),
        ),
      ],
    );
  }
}
