import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

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
///  - [saved] — they confirmed: a successful copy, or "I've saved it". Just close.
///  - [dismissed] — they walked away: the ✕, or the back gesture. Show them the
///    key is gone and offer to issue another.
///
/// Getting this backwards means telling someone who *just copied the key* that
/// they lost it — or, far worse, letting someone who lost it believe they didn't.
enum InviteRevealResult { saved, dismissed }

/// The hero. A secret shown once, and honest about it.
Future<InviteRevealResult> showInviteReveal(
  BuildContext context,
  Invite invite,
) async {
  final result = await showLycSheet<InviteRevealResult>(
    context: context,
    // Not dismissible by drag or scrim tap. A stray swipe here throws away the
    // only copy of an unrecoverable credential; closing has to be a decision.
    // (The back gesture still works — Android's contract — and lands as
    // `dismissed`, which is exactly the recovery path.)
    dismissible: false,
    builder: (context) => _InviteRevealSheet(invite: invite),
  );
  return result ?? InviteRevealResult.dismissed;
}

class _InviteRevealSheet extends StatefulWidget {
  const _InviteRevealSheet({required this.invite});
  final Invite invite;

  @override
  State<_InviteRevealSheet> createState() => _InviteRevealSheetState();
}

class _InviteRevealSheetState extends State<_InviteRevealSheet> {
  bool _copied = false;
  bool _copyFailed = false;

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

    return Column(
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
              onPressed: () =>
                  Navigator.of(context).pop(InviteRevealResult.dismissed),
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
