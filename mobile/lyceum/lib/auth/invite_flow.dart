import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_providers.dart';
import '../api/models.dart';
import '../api/server_store.dart';
import '../widgets/invite_reveal.dart';
import 'invite_token.dart';

/// The reveal → "that key is gone" → re-issue loop.
///
/// Two screens hand out keys — Household to housemates, Settings to your own next
/// device (LYCM-105) — and the delicate part is the same either way: a plaintext
/// secret that exists only in memory, a dismissal that may or may not have lost
/// it, and a recovery path that has to re-mint down the same route the key came
/// from. Keeping two copies of that in step by hand is how one of them ends up
/// telling someone their key is gone when it isn't.
///
/// [self] says whose key this is, and decides two things: how the sheet reads,
/// and **which route re-issues it** — your own goes back through `/auth/invite`, a
/// housemate's through `/admin`. It is told, not inferred: deriving it by
/// comparing against the signed-in account would silently answer "not yours" if
/// that account were ever null or still resolving, sending a re-issue of your own
/// device key down the owner-only admin route to 403 for every member. Both
/// callers know the answer for certain at the point they mint.
///
/// [onMinted] fires for every key this shows, including re-issues, so a caller can
/// re-read whatever the mint changed (the household list, for one — an outstanding
/// invite is what turns a row "pending").
/// The link the reveal offers as a scannable QR, or null when there is none
/// worth offering (LYCM-88, LYCM-102).
///
/// The server's own answer wins when it has one: the address *this* phone
/// happens to use may be a LAN one, and handing that out as a QR produces an
/// invite that works only for someone already on the network — which is
/// precisely not the person being invited. [serverUrl] stays the fallback, since
/// on a LAN or in dev it is the only address there is. Empty (which shouldn't
/// happen once signed in) means no QR at all rather than one pointing nowhere.
///
/// Split out from [runInviteReveal] so the precedence can be read and tested
/// without standing up a modal.
String? revealSignInUrl(Invite invite, String serverUrl) {
  final advertised = invite.signInUrl;
  if (advertised != null) return advertised;
  return serverUrl.isEmpty ? null : inviteSignInUrl(serverUrl, invite.token);
}

Future<void> runInviteReveal(
  BuildContext context,
  WidgetRef ref,
  Invite invite, {
  required bool self,
  VoidCallback? onMinted,
}) async {
  onMinted?.call();

  final signInUrl = revealSignInUrl(invite, ref.read(serverUrlProvider));

  final result = await showInviteReveal(
    context,
    invite,
    signInUrl: signInUrl,
    self: self,
  );
  if (!context.mounted || result == InviteRevealResult.saved) return;

  // Dismissed without copying. Not an error — the one case where a person can
  // silently lose something unrecoverable, so it gets an explicit second act.
  final name = invite.user.displayName.trim().isEmpty
      ? invite.user.email
      : invite.user.displayName.trim();
  final reissue = await showInviteLostSheet(context, name, self: self);
  if (!context.mounted || !reissue) return;

  try {
    final client = ref.read(lyceumClientProvider);
    final fresh = self
        ? await client.requestDeviceInvite()
        : await client.reinviteMember(invite.user.id);
    if (context.mounted) {
      // Same `self` all the way down: a re-issue of your own key is still yours.
      await runInviteReveal(
        context,
        ref,
        fresh,
        self: self,
        onMinted: onMinted,
      );
    }
  } catch (e) {
    if (context.mounted) {
      ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('$e')));
    }
  }
}
