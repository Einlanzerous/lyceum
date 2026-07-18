import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:qr_flutter/qr_flutter.dart';

import '../../api/models.dart';
import '../../theme/lyceum_colors.dart';
import '../../theme/lyceum_theme.dart';
import '../../widgets/lyc_sheet.dart';

/// How the reveal was closed.
///
/// This is the whole ballgame. The invite is plaintext exactly once — the server
/// keeps only a hash — so "did they actually get it out of here?" decides whether
/// the next thing they see is a shrug or a recovery path.
///
///  - [saved] — the key got out: they copied it, or said they'd saved it.
///  - [dismissed] — they walked away without ever taking it. Show them it's gone
///    and offer to issue another.
///
/// **A successful copy means [saved], however they then close the sheet.** The ✕
/// is not evidence that the key was lost — someone who copies the key, switches
/// to a chat app to send it, and comes back to tidy up has done everything right.
/// Telling *them* "that invite is gone, issue another" is not a harmless mistake:
/// they believe us, issue another, and the fresh mint invalidates the key their
/// housemate is already holding.
enum InviteRevealResult { saved, dismissed }

/// The hero. A secret shown once, and honest about it.
///
/// [signInUrl] is the scannable `<origin>/sign-in?token=…` link the caller builds
/// from the configured server (LYCM-88); when present, the key is also offered as
/// a QR. Passed in rather than read here so this sheet stays a plain,
/// provider-free widget.
Future<InviteRevealResult> showInviteReveal(
  BuildContext context,
  Invite invite, {
  String? signInUrl,
}) async {
  final result = await showLycSheet<InviteRevealResult>(
    context: context,
    // Not dismissible by drag or scrim tap. A stray swipe here throws away the
    // only copy of an unrecoverable credential; closing has to be a decision.
    // (The back gesture still works — Android's contract — and lands as
    // `dismissed`, which is exactly the recovery path.)
    dismissible: false,
    builder: (context) => _InviteRevealSheet(invite: invite, signInUrl: signInUrl),
  );
  return result ?? InviteRevealResult.dismissed;
}

class _InviteRevealSheet extends StatefulWidget {
  const _InviteRevealSheet({required this.invite, this.signInUrl});
  final Invite invite;
  final String? signInUrl;

  @override
  State<_InviteRevealSheet> createState() => _InviteRevealSheetState();
}

class _InviteRevealSheetState extends State<_InviteRevealSheet> {
  bool _copied = false;
  bool _copyFailed = false;
  bool _codeCopied = false;

  /// The pairing code grouped XXXX-XXXX for reading aloud and typing (LYCM-88);
  /// the sign-in field strips the hyphen back out.
  String get _groupedCode {
    final c = widget.invite.pairingCode;
    return c.length == 8 ? '${c.substring(0, 4)}-${c.substring(4)}' : c;
  }

  Future<void> _copyCode() async {
    try {
      await Clipboard.setData(ClipboardData(text: widget.invite.pairingCode));
      if (mounted) setState(() => _codeCopied = true);
    } catch (_) {
      // The code is short and on screen — unlike the key, a blocked clipboard is
      // no loss; it can just be read aloud.
    }
  }

  /// The one question this sheet exists to answer: did the key get out?
  ///
  /// Consulted by *every* exit — the ✕ and the back gesture included, not just
  /// the two buttons that say so out loud.
  InviteRevealResult get _result =>
      _copied ? InviteRevealResult.saved : InviteRevealResult.dismissed;

  String get _name {
    final n = widget.invite.user.displayName.trim();
    return n.isEmpty ? widget.invite.user.email : n;
  }

  Future<void> _copy() async {
    try {
      await Clipboard.setData(ClipboardData(text: widget.invite.token));
      if (!mounted) return;
      setState(() {
        _copied = true;
        _copyFailed = false;
      });
    } catch (_) {
      if (!mounted) return;
      setState(() => _copyFailed = true);
    }
  }

  Future<void> _copyAndClose() async {
    await _copy();
    // Never close on a failed copy. Closing now would destroy the only copy of
    // a key that cannot be shown again — the button's promise ("& close") is not
    // worth honouring when the first half of it didn't happen.
    if (!mounted || _copyFailed) return;
    Navigator.of(context).pop(InviteRevealResult.saved);
  }

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    final signInUrl = widget.signInUrl;

    return PopScope(
      // Android's back gesture is the documented way out of a sheet that refuses
      // scrim-taps and drags. Left to itself it pops `null`, which would read as
      // "walked away" even from someone who had just copied the key — so it is
      // intercepted and answered with the truth.
      canPop: false,
      onPopInvokedWithResult: (didPop, _) {
        if (didPop) return;
        Navigator.of(context).pop(_result);
      },
      child: Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Row(
          children: [
            CircleAvatar(
              radius: 20,
              backgroundColor: lyc.brass,
              child: Text(
                widget.invite.user.initial,
                style: TextStyle(
                  color: lyc.onBrass,
                  fontFamily: kDisplayFont,
                  fontWeight: FontWeight.w700,
                  fontSize: 16,
                ),
              ),
            ),
            const SizedBox(width: 12),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    'INVITE CREATED',
                    style: TextStyle(
                      fontFamily: kDisplayFont,
                      fontSize: 10,
                      fontWeight: FontWeight.w700,
                      letterSpacing: 2,
                      color: lyc.brass,
                    ),
                  ),
                  const SizedBox(height: 2),
                  Text(
                    'A key for $_name',
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(
                      fontFamily: kDisplayFont,
                      fontWeight: FontWeight.w800,
                      fontSize: 20,
                      color: lyc.text,
                    ),
                  ),
                ],
              ),
            ),
            IconButton(
              tooltip: 'Close',
              // Not unconditionally "dismissed": if they copied the key, they
              // have it, and the ✕ is just how they tidied up.
              onPressed: () => Navigator.of(context).pop(_result),
              icon: Icon(Icons.close_rounded, size: 20, color: lyc.muted),
            ),
          ],
        ),
        const SizedBox(height: 14),
        Text(
          "Hand this key to $_name. When they paste it on their device, they're "
          "in. It's the only credential — treat it like a house key, not a link.",
          style: TextStyle(fontSize: 13.5, height: 1.55, color: lyc.muted),
        ),
        const SizedBox(height: 20),

        Text(
          'THE INVITE KEY',
          style: TextStyle(
            fontSize: 10,
            fontWeight: FontWeight.w600,
            letterSpacing: 1.4,
            color: lyc.dim,
          ),
        ),
        const SizedBox(height: 9),
        Container(
          width: double.infinity,
          padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 15),
          decoration: BoxDecoration(
            color: lyc.bg,
            border: Border.all(
              color: (_copied ? lyc.success : lyc.brass).withValues(alpha: 0.42),
            ),
            borderRadius: BorderRadius.circular(LycRadii.card),
          ),
          // Selectable, so a blocked clipboard is survivable by hand.
          child: SelectableText(
            widget.invite.token,
            style: TextStyle(
              fontFamily: 'monospace',
              fontSize: 15,
              height: 1.4,
              color: _copied ? lyc.success : lyc.brassBright,
            ),
          ),
        ),
        const SizedBox(height: 12),
        SizedBox(
          width: double.infinity,
          child: OutlinedButton.icon(
            onPressed: _copy,
            icon: Icon(
              _copied ? Icons.check_rounded : Icons.copy_rounded,
              size: 17,
            ),
            label: Text(_copied ? 'Copied' : 'Copy key'),
          ),
        ),

        // The type-it-in path: a short code for when scanning and pasting are
        // both off the table (reading it aloud, say). It expires far sooner than
        // the key above.
        const SizedBox(height: 16),
        _PairingCode(
          code: _groupedCode,
          copied: _codeCopied,
          onCopy: _copyCode,
        ),

        // The same key as a QR, so $_name can scan it from their phone's camera
        // instead of copying text across devices (LYCM-88). The caller encodes
        // the sign-in URL served by this library, so a stock camera app can
        // complete the flow.
        if (signInUrl != null && signInUrl.isNotEmpty) _InviteQr(url: signInUrl),

        const SizedBox(height: 14),
        Wrap(
          spacing: 16,
          runSpacing: 6,
          children: const [
            _Meta('Expires in 7 days'),
            _Meta('Works once'),
            _Meta('One device'),
          ],
        ),

        const SizedBox(height: 18),
        if (_copyFailed)
          LycNotice(
            tone: LycTone.error,
            icon: Icons.error_outline_rounded,
            child: Text(
              "Couldn't reach the clipboard. Select the key above and copy it by "
              'hand — this is the only time it exists.',
              style: TextStyle(fontSize: 12.5, height: 1.5, color: lyc.muted),
            ),
          )
        else if (_copied)
          LycNotice(
            tone: LycTone.success,
            icon: Icons.check_circle_outline_rounded,
            child: Text(
              'Copied to clipboard — now hand it to $_name. Closing this is safe '
              "once you've sent it.",
              style: TextStyle(fontSize: 12.5, height: 1.5, color: lyc.muted),
            ),
          )
        else
          LycNotice(
            tone: LycTone.warning,
            icon: Icons.warning_amber_rounded,
            child: Text.rich(
              TextSpan(
                children: [
                  TextSpan(
                    text: "This is the only time you'll see this key.",
                    style: TextStyle(
                      fontWeight: FontWeight.w700,
                      color: lyc.brassBright,
                    ),
                  ),
                  TextSpan(
                    text: " Copy it before you close — we can't show it again. "
                        'Lost it? Just issue $_name another.',
                  ),
                ],
              ),
              style: TextStyle(fontSize: 12.5, height: 1.5, color: lyc.muted),
            ),
          ),

        const SizedBox(height: 20),
        Row(
          children: [
            Expanded(
              child: FilledButton(
                onPressed: _copyAndClose,
                child: const Text('Copy & close'),
              ),
            ),
            const SizedBox(width: 10),
            OutlinedButton(
              onPressed: () =>
                  Navigator.of(context).pop(InviteRevealResult.saved),
              child: const Text("I've saved it"),
            ),
          ],
        ),
      ],
      ),
    );
  }
}

/// The invite as a QR on a white quiet-zone tile (LYCM-88). White is not a theme
/// choice — QR contrast has to survive the app's dark surfaces to stay scannable.
class _InviteQr extends StatelessWidget {
  const _InviteQr({required this.url});
  final String url;

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    return Padding(
      padding: const EdgeInsets.only(top: 16),
      child: Column(
        children: [
          Container(
            padding: const EdgeInsets.all(12),
            decoration: BoxDecoration(
              color: Colors.white,
              borderRadius: BorderRadius.circular(LycRadii.card),
              border: Border.all(color: lyc.borderStrong),
            ),
            child: QrImageView(
              data: url,
              version: QrVersions.auto,
              size: 176,
              backgroundColor: Colors.white,
              // Fixed dark modules on the white tile, independent of app theme.
              eyeStyle: const QrEyeStyle(
                eyeShape: QrEyeShape.square,
                color: Color(0xFF000000),
              ),
              dataModuleStyle: const QrDataModuleStyle(
                dataModuleShape: QrDataModuleShape.square,
                color: Color(0xFF000000),
              ),
            ),
          ),
          const SizedBox(height: 9),
          Text(
            'Or have them scan this with their camera',
            textAlign: TextAlign.center,
            style: TextStyle(fontSize: 11.5, color: lyc.dim),
          ),
        ],
      ),
    );
  }
}

/// The invite as a short code with a copy affordance (LYCM-88).
class _PairingCode extends StatelessWidget {
  const _PairingCode({
    required this.code,
    required this.copied,
    required this.onCopy,
  });

  final String code;
  final bool copied;
  final VoidCallback onCopy;

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Text(
          'OR A SHORT CODE — EXPIRES IN 15 MIN',
          style: TextStyle(
            fontSize: 10,
            fontWeight: FontWeight.w600,
            letterSpacing: 1.4,
            color: lyc.dim,
          ),
        ),
        const SizedBox(height: 8),
        Row(
          children: [
            Expanded(
              child: Container(
                padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 13),
                decoration: BoxDecoration(
                  color: lyc.bg,
                  border: Border.all(color: lyc.borderStrong),
                  borderRadius: BorderRadius.circular(LycRadii.card),
                ),
                child: SelectableText(
                  code,
                  style: TextStyle(
                    fontFamily: 'monospace',
                    fontSize: 20,
                    fontWeight: FontWeight.w700,
                    letterSpacing: 3,
                    color: lyc.text,
                  ),
                ),
              ),
            ),
            const SizedBox(width: 10),
            OutlinedButton.icon(
              onPressed: onCopy,
              icon: Icon(copied ? Icons.check_rounded : Icons.copy_rounded, size: 16),
              label: Text(copied ? 'Copied' : 'Copy'),
            ),
          ],
        ),
      ],
    );
  }
}

class _Meta extends StatelessWidget {
  const _Meta(this.text);
  final String text;

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        Container(
          width: 6,
          height: 6,
          decoration: BoxDecoration(color: lyc.brass, shape: BoxShape.circle),
        ),
        const SizedBox(width: 7),
        Text(
          text,
          style: TextStyle(
            fontSize: 12,
            fontWeight: FontWeight.w600,
            color: lyc.muted,
          ),
        ),
      ],
    );
  }
}

/// The recovery path: they closed the reveal without taking the key.
///
/// The tone matters. Nothing is broken and nothing is lost except a string that
/// was always meant to be disposable — the *account* is still there, waiting. So
/// this explains rather than apologises, and puts the fix one tap away.
Future<bool> showInviteLostSheet(BuildContext context, String name) async {
  final reissue = await showLycSheet<bool>(
    context: context,
    builder: (context) => _InviteLostSheet(name: name),
  );
  return reissue ?? false;
}

class _InviteLostSheet extends StatelessWidget {
  const _InviteLostSheet({required this.name});
  final String name;

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Row(
          children: [
            Icon(Icons.lock_outline_rounded, size: 19, color: lyc.muted),
            const SizedBox(width: 10),
            Text(
              'That invite is gone',
              style: Theme.of(context).textTheme.titleLarge,
            ),
          ],
        ),
        const SizedBox(height: 14),
        Container(
          width: double.infinity,
          padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 14),
          decoration: BoxDecoration(
            color: lyc.bg,
            border: Border.all(color: lyc.borderStrong, style: BorderStyle.solid),
            borderRadius: BorderRadius.circular(LycRadii.card),
          ),
          child: Text(
            'lyc_•••••••••••••••••••••••',
            style: TextStyle(
              fontFamily: 'monospace',
              fontSize: 14.5,
              letterSpacing: 0.5,
              color: lyc.dimmer,
            ),
          ),
        ),
        const SizedBox(height: 14),
        Text(
          "For security, an invite is shown only once and we can't display it "
          "again. $name's account is still here and waiting — issue a fresh key "
          "whenever you're ready.",
          style: TextStyle(fontSize: 13, height: 1.55, color: lyc.muted),
        ),
        const SizedBox(height: 20),
        FilledButton(
          onPressed: () => Navigator.of(context).pop(true),
          child: Text('Issue another invite for $name'),
        ),
        const SizedBox(height: 8),
        TextButton(
          onPressed: () => Navigator.of(context).pop(false),
          child: const Text('Not now'),
        ),
      ],
    );
  }
}
