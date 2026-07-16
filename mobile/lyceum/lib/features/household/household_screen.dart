import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../api/api_providers.dart';
import '../../api/client.dart';
import '../../api/models.dart';
import '../../api/server_store.dart';
import '../../auth/auth_controller.dart';
import '../../auth/invite_token.dart';
import '../../auth/relative_time.dart';
import '../../theme/lyceum_colors.dart';
import '../../theme/lyceum_theme.dart';
import '../../widgets/lyc_sheet.dart';
import 'invite_reveal.dart';

final membersProvider = FutureProvider<List<Member>>(
  (ref) => ref.watch(lyceumClientProvider).listMembers(),
);

/// A household row's second line — the only place the difference between a
/// housemate, an invite nobody redeemed, and someone who has signed out
/// everywhere is visible.
///
/// [now] is injectable so the boundaries can be tested.
String memberSubtitle(Member m, {DateTime? now}) {
  if (m.isOwner) return '${m.account.email} · ${deviceCount(m.sessionCount)}';

  if (m.isPending) {
    return 'Invite pending · ${inviteExpiresIn(m.inviteExpiresAt, now: now)} · '
        'never signed in';
  }

  // Somebody who signed out of every device they had. The server derives
  // last_seen_at from their sessions, so revoking the last one takes the
  // timestamp with it — which is how this row came to read
  // "Active · 0 devices · never signed in": three claims, two of them false, and
  // all three at odds with one another. We genuinely cannot say when they were
  // last here, so we don't guess — we say the one thing we know.
  if (m.sessionCount == 0) return 'No devices signed in';

  return 'Active · ${deviceCount(m.sessionCount)} · '
      '${memberSeenAt(m.lastSeenAt, now: now)}';
}

/// The owner's view of the household (LYCM-804). Owner only — a member is never
/// offered this, and the router sends them home if they somehow arrive.
class HouseholdScreen extends ConsumerStatefulWidget {
  const HouseholdScreen({super.key});

  @override
  ConsumerState<HouseholdScreen> createState() => _HouseholdScreenState();
}

class _HouseholdScreenState extends ConsumerState<HouseholdScreen> {
  bool _inviting = false;

  /// Reveal a freshly minted key, then act on how it was closed.
  ///
  /// Dismissing without copying is not an error — it is the one case where a
  /// person can silently lose something unrecoverable, so it gets an explicit
  /// second act rather than a shrug.
  Future<void> _reveal(Invite invite) async {
    ref.invalidate(membersProvider);
    // Offer the key as a scannable QR too, pointing at this library's sign-in
    // route (LYCM-88). Empty server URL (shouldn't happen once signed in) simply
    // omits the QR.
    final serverUrl = ref.read(serverUrlProvider);
    final signInUrl = serverUrl.isEmpty ? null : inviteSignInUrl(serverUrl, invite.token);
    final result = await showInviteReveal(context, invite, signInUrl: signInUrl);
    if (!mounted || result == InviteRevealResult.saved) return;

    final name = invite.user.displayName.trim().isEmpty
        ? invite.user.email
        : invite.user.displayName.trim();
    final reissue = await showInviteLostSheet(context, name);
    if (!mounted || !reissue) return;

    try {
      final fresh = await ref.read(lyceumClientProvider).reinviteMember(invite.user.id);
      if (mounted) await _reveal(fresh); // straight back to the reveal, new key
    } catch (e) {
      _toast('$e');
    }
  }

  Future<void> _reinvite(Member m) async {
    try {
      final invite = await ref.read(lyceumClientProvider).reinviteMember(m.id);
      if (mounted) await _reveal(invite);
    } catch (e) {
      _toast('$e');
    }
  }

  Future<void> _remove(Member m) async {
    final confirmed = await showLycSheet<bool>(
      context: context,
      builder: (context) => _RemoveSheet(name: m.displayName),
    );
    if (confirmed != true || !mounted) return;
    try {
      await ref.read(lyceumClientProvider).removeMember(m.id);
      ref.invalidate(membersProvider);
    } catch (e) {
      _toast('$e');
    }
  }

  /// Collect who to invite, then mint the key **here**, not inside the sheet.
  ///
  /// The sheet can be swiped away or scrim-tapped, and a home server on a slow
  /// LAN leaves a real window in which to do it. If the `POST` lived in the sheet,
  /// dismissing it mid-flight would land the response on a dead widget: the
  /// account would exist on the server, its one-time plaintext key would be
  /// dropped on the floor, and nobody would be shown the recovery path — because
  /// from the screen's point of view nothing was ever created.
  ///
  /// So the sheet only gathers an email and a name. Whatever it hands back, the
  /// screen mints from — and the screen is still there to show the reveal.
  Future<void> _openInviteForm() async {
    final who = await showLycSheet<({String email, String name})>(
      context: context,
      builder: (context) => const _InviteForm(),
    );
    if (who == null || !mounted) return;

    setState(() => _inviting = true);
    try {
      final invite = await ref
          .read(lyceumClientProvider)
          .inviteMember(email: who.email, displayName: who.name);
      if (mounted) await _reveal(invite);
    } on ApiException catch (e) {
      _toast(
        e.isDuplicate
            ? 'Someone on this server already uses that email.'
            : e.message,
      );
    } catch (e) {
      _toast('$e');
    } finally {
      if (mounted) setState(() => _inviting = false);
    }
  }

  void _toast(String message) {
    if (!mounted) return;
    ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(message)));
  }

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    final members = ref.watch(membersProvider);
    final me = ref.watch(authControllerProvider).user;

    // The auth-off state is not an error to apologise for — it is the server
    // refusing to mint credentials it cannot attribute. It replaces the whole
    // screen: there is no list to show and nothing here can be changed.
    final adminOff = members.hasError && members.error is AdminDisabledException;

    return Scaffold(
      body: SafeArea(
        minimum: EdgeInsets.only(top: MediaQuery.viewPaddingOf(context).top),
        child: RefreshIndicator(
          color: lyc.brass,
          onRefresh: () async => ref.invalidate(membersProvider),
          child: ListView(
            padding: const EdgeInsets.fromLTRB(20, 16, 20, 48),
            children: [
              _BackPill(onTap: () => context.pop()),
              const SizedBox(height: 24),
              Text(
                'HOUSEHOLD',
                style: TextStyle(
                  fontSize: 11.5,
                  fontWeight: FontWeight.w700,
                  letterSpacing: 3,
                  color: lyc.brass,
                ),
              ),
              const SizedBox(height: 6),
              Text(
                'The people on this server',
                style: Theme.of(context).textTheme.headlineLarge,
              ),
              const SizedBox(height: 24),

              if (adminOff)
                const _AdminOffPanel()
              else ...[
                SizedBox(
                  width: double.infinity,
                  child: FilledButton.icon(
                    onPressed: _inviting ? null : _openInviteForm,
                    icon: const Icon(Icons.person_add_alt_1_rounded, size: 18),
                    label: Text(_inviting ? 'Creating…' : 'Invite someone'),
                  ),
                ),
                const SizedBox(height: 20),
                members.when(
                  loading: () => Text(
                    'Loading the household…',
                    style: TextStyle(fontSize: 13, color: lyc.dim),
                  ),
                  error: (e, _) => Text(
                    '$e',
                    style: TextStyle(fontSize: 12.5, color: lyc.error),
                  ),
                  data: (list) => Column(
                    children: [
                      for (final m in list) ...[
                        _MemberCard(
                          member: m,
                          isMe: m.id == me?.id,
                          onReinvite: () => _reinvite(m),
                          onRemove: () => _remove(m),
                        ),
                        const SizedBox(height: 10),
                      ],
                    ],
                  ),
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }
}

class _MemberCard extends StatelessWidget {
  const _MemberCard({
    required this.member,
    required this.isMe,
    required this.onReinvite,
    required this.onRemove,
  });

  final Member member;
  final bool isMe;
  final VoidCallback onReinvite;
  final VoidCallback onRemove;

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    final name = member.displayName.trim().isEmpty
        ? member.account.email
        : member.displayName;

    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: lyc.surface,
        borderRadius: BorderRadius.circular(LycRadii.card),
        border: Border.all(color: lyc.border),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              CircleAvatar(
                radius: 19,
                backgroundColor: member.isPending ? lyc.surfaceRaised : lyc.brass,
                child: Text(
                  member.account.initial,
                  style: TextStyle(
                    color: member.isPending ? lyc.muted : lyc.onBrass,
                    fontFamily: kDisplayFont,
                    fontWeight: FontWeight.w700,
                    fontSize: 15,
                  ),
                ),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      children: [
                        Flexible(
                          child: Text(
                            name,
                            overflow: TextOverflow.ellipsis,
                            style: TextStyle(
                              fontSize: 15,
                              fontWeight: FontWeight.w700,
                              color: lyc.text,
                            ),
                          ),
                        ),
                        if (member.isOwner) ...[
                          const SizedBox(width: 8),
                          _Chip(
                            label: isMe ? 'Owner · You' : 'Owner',
                            color: lyc.brass,
                          ),
                        ] else if (member.isPending) ...[
                          const SizedBox(width: 8),
                          _Chip(label: 'Pending', color: lyc.brass),
                        ],
                      ],
                    ),
                    const SizedBox(height: 3),
                    Text(
                      memberSubtitle(member),
                      style: TextStyle(fontSize: 12, height: 1.4, color: lyc.dim),
                    ),
                  ],
                ),
              ),
            ],
          ),
          const SizedBox(height: 12),
          if (member.isOwner)
            Text(
              "Can't be removed",
              style: TextStyle(
                fontSize: 12,
                fontStyle: FontStyle.italic,
                color: lyc.dimmer,
              ),
            )
          else
            Row(
              children: [
                Expanded(
                  child: OutlinedButton(
                    onPressed: onReinvite,
                    child: const Text('Re-invite'),
                  ),
                ),
                const SizedBox(width: 10),
                Expanded(
                  child: OutlinedButton(
                    onPressed: onRemove,
                    style: OutlinedButton.styleFrom(
                      foregroundColor: lyc.error,
                      side: BorderSide(color: lyc.error.withValues(alpha: 0.4)),
                    ),
                    child: const Text('Remove'),
                  ),
                ),
              ],
            ),
        ],
      ),
    );
  }
}

/// Gathers who to invite. Deliberately does no network work of its own — see
/// [_HouseholdScreenState._openInviteForm]. A sheet that can be swiped away is no
/// place to be holding the only copy of a credential.
class _InviteForm extends StatefulWidget {
  const _InviteForm();

  @override
  State<_InviteForm> createState() => _InviteFormState();
}

class _InviteFormState extends State<_InviteForm> {
  final _email = TextEditingController();
  final _name = TextEditingController();

  @override
  void dispose() {
    _email.dispose();
    _name.dispose();
    super.dispose();
  }

  void _submit() {
    final email = _email.text.trim();
    if (email.isEmpty) return;
    Navigator.of(context).pop((email: email, name: _name.text.trim()));
  }

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Text('Invite someone', style: Theme.of(context).textTheme.titleLarge),
        const SizedBox(height: 14),
        TextField(
          controller: _email,
          keyboardType: TextInputType.emailAddress,
          autocorrect: false,
          autofocus: true,
          onChanged: (_) => setState(() {}),
          onSubmitted: (_) => _submit(),
          decoration: const InputDecoration(
            labelText: 'Email',
            hintText: 'theo@home.lan',
          ),
        ),
        const SizedBox(height: 12),
        TextField(
          controller: _name,
          textCapitalization: TextCapitalization.words,
          onSubmitted: (_) => _submit(),
          decoration: const InputDecoration(
            labelText: 'Name',
            hintText: 'Theo (optional)',
          ),
        ),
        const SizedBox(height: 10),
        Text(
          "They'll get a one-time key to paste on their device. It's shown once.",
          style: TextStyle(fontSize: 12.5, height: 1.5, color: lyc.dim),
        ),
        const SizedBox(height: 18),
        Row(
          children: [
            Expanded(
              child: OutlinedButton(
                onPressed: () => Navigator.of(context).pop(),
                child: const Text('Cancel'),
              ),
            ),
            const SizedBox(width: 10),
            Expanded(
              child: FilledButton(
                onPressed: _email.text.trim().isEmpty ? null : _submit,
                child: const Text('Create invite'),
              ),
            ),
          ],
        ),
      ],
    );
  }
}

/// Removing someone deletes their reading positions — and *only* those. People
/// hesitate here because they think they might be deleting books, so the copy
/// says outright that the shelf is untouched.
class _RemoveSheet extends StatelessWidget {
  const _RemoveSheet({required this.name});
  final String name;

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Text('Remove $name?', style: Theme.of(context).textTheme.titleLarge),
        const SizedBox(height: 12),
        Text.rich(
          TextSpan(
            children: [
              TextSpan(
                text: "This deletes $name's reading positions and bookmarks on "
                    'every book. ',
              ),
              TextSpan(
                text: 'The shared shelf is untouched',
                style: TextStyle(fontWeight: FontWeight.w700, color: lyc.reading),
              ),
              const TextSpan(
                text: " — no titles are lost. This can't be undone.",
              ),
            ],
          ),
          style: TextStyle(fontSize: 13.5, height: 1.55, color: lyc.muted),
        ),
        const SizedBox(height: 20),
        Row(
          children: [
            Expanded(
              child: OutlinedButton(
                onPressed: () => Navigator.of(context).pop(false),
                child: const Text('Cancel'),
              ),
            ),
            const SizedBox(width: 10),
            Expanded(
              child: FilledButton(
                onPressed: () => Navigator.of(context).pop(true),
                style: FilledButton.styleFrom(
                  backgroundColor: lyc.error,
                  foregroundColor: lyc.onBrass,
                ),
                child: Text('Remove $name', overflow: TextOverflow.ellipsis),
              ),
            ),
          ],
        ),
      ],
    );
  }
}

/// `LYCEUM_AUTH=false`. Every `/admin/*` route 403s, and the fix is deliberately
/// not in this app: a server that cannot tell who is asking must not be able to
/// mint credentials on a stranger's say-so, or anyone who could reach the port
/// could invite themselves in and still hold the session after the operator
/// closed the door.
class _AdminOffPanel extends StatelessWidget {
  const _AdminOffPanel();

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    return Container(
      padding: const EdgeInsets.all(20),
      decoration: BoxDecoration(
        color: lyc.surface,
        borderRadius: BorderRadius.circular(LycRadii.card),
        border: Border.all(color: lyc.border),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              Icon(Icons.lock_outline_rounded, size: 19, color: lyc.muted),
              const SizedBox(width: 10),
              Text(
                'Household admin is off',
                style: Theme.of(context).textTheme.titleMedium,
              ),
            ],
          ),
          const SizedBox(height: 12),
          Text(
            'Accounts exist on this server, but managing them is switched off. '
            'Nothing here can be changed until an operator turns it on.',
            style: TextStyle(fontSize: 13.5, height: 1.55, color: lyc.muted),
          ),
          const SizedBox(height: 16),
          Container(
            width: double.infinity,
            padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
            decoration: BoxDecoration(
              color: lyc.bg,
              borderRadius: BorderRadius.circular(LycRadii.cover),
              border: Border.all(color: lyc.border),
            ),
            child: SelectableText(
              '\$ export LYCEUM_AUTH=true\n# then restart the server',
              style: TextStyle(
                fontFamily: 'monospace',
                fontSize: 12.5,
                height: 1.6,
                color: lyc.brassBright,
              ),
            ),
          ),
          const SizedBox(height: 14),
          Text(
            'This has to happen on the machine running Lyceum — there’s no remote '
            'switch, by design.',
            style: TextStyle(fontSize: 12.5, height: 1.5, color: lyc.dim),
          ),
        ],
      ),
    );
  }
}

class _Chip extends StatelessWidget {
  const _Chip({required this.label, required this.color});
  final String label;
  final Color color;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 2),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.14),
        border: Border.all(color: color.withValues(alpha: 0.35)),
        borderRadius: BorderRadius.circular(LycRadii.pill),
      ),
      child: Text(
        label,
        style: TextStyle(
          fontSize: 10,
          fontWeight: FontWeight.w700,
          letterSpacing: 0.3,
          color: color,
        ),
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
    return Align(
      alignment: Alignment.centerLeft,
      child: GestureDetector(
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
              Text('Settings', style: TextStyle(fontSize: 13, color: lyc.text)),
            ],
          ),
        ),
      ),
    );
  }
}
