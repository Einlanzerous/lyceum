import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../api/api_providers.dart';
import '../../api/models.dart';
import '../../auth/auth_controller.dart';
import '../../auth/relative_time.dart';
import '../../theme/lyceum_colors.dart';
import '../../theme/lyceum_theme.dart';
import '../../widgets/lyc_sheet.dart';

/// This device's signed-in siblings. Only meaningful when the server enforces
/// auth — with enforcement off nobody holds a session at all.
final devicesProvider = FutureProvider<List<DeviceSession>>((ref) async {
  if (!ref.watch(enforcedProvider)) return const [];
  return ref.watch(lyceumClientProvider).listDevices();
});

/// The account, as seen from Settings.
///
/// Replaces the LYCM-700 `_ProfileEditor`, which persisted a display name to
/// SharedPreferences because there were no accounts to hang one on. The name now
/// lives on the server and follows the person to every device they sign in on.
class AccountSection extends ConsumerStatefulWidget {
  const AccountSection({super.key});

  @override
  ConsumerState<AccountSection> createState() => _AccountSectionState();
}

class _AccountSectionState extends ConsumerState<AccountSection> {
  final _name = TextEditingController();
  bool _editing = false;
  bool _saving = false;
  bool _signingOut = false;
  String? _error;

  @override
  void dispose() {
    _name.dispose();
    super.dispose();
  }

  Future<void> _save() async {
    final name = _name.text.trim();
    if (name.isEmpty || _saving) return;
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      await ref.read(authControllerProvider.notifier).rename(name);
      if (mounted) setState(() => _editing = false);
    } catch (e) {
      if (mounted) setState(() => _error = '$e');
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  Future<void> _signOut() async {
    final label = ref
        .read(devicesProvider)
        .maybeWhen(
          data: (devices) => devices
              .where((d) => d.current)
              .map((d) => d.deviceLabel)
              .firstOrNull,
          orElse: () => null,
        );

    final confirmed = await showLycSheet<bool>(
      context: context,
      builder: (context) => _SignOutSheet(deviceLabel: label),
    );
    if (confirmed != true || !mounted) return;

    setState(() => _signingOut = true);
    try {
      await ref.read(authControllerProvider.notifier).signOut();
      // The router bounces us to the front door on its own; the shelf must be
      // re-asked for once somebody signs in again.
      ref.invalidate(devicesProvider);
    } catch (_) {
      // The local token is already gone (AuthController.signOut drops it in a
      // finally) — this device is signed out either way. A server that didn't
      // hear about it will still honour the revoke on the next reachable call,
      // and the session shows in "your devices" until then.
    } finally {
      if (mounted) setState(() => _signingOut = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    final auth = ref.watch(authControllerProvider);
    final enforced = ref.watch(enforcedProvider);
    final user = auth.user;
    if (user == null) return const SizedBox.shrink();

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        if (_editing) _editor(user) else _identity(user),

        if (auth.isOwner) ...[
          Divider(height: 28, color: lyc.border),
          _Row(
            title: 'Household',
            hint: 'Invite or remove the people who share this library.',
            action: OutlinedButton(
              onPressed: () => context.push('/household'),
              child: const Text('Manage'),
            ),
          ),
        ],

        // Hidden entirely when the server doesn't enforce auth. Signing out of a
        // library that issues no invites strands you on a front door you cannot
        // get past, with your own shelf on the other side of it.
        if (enforced) ...[
          Divider(height: 28, color: lyc.border),
          _Row(
            title: 'Sign out',
            hint: 'This device only. Your other devices stay signed in and keep '
                'syncing.',
            action: OutlinedButton(
              onPressed: _signingOut ? null : _signOut,
              child: Text(_signingOut ? 'Signing out…' : 'Sign out'),
            ),
          ),
        ],
      ],
    );
  }

  Widget _identity(Account user) {
    final lyc = context.lyc;
    return Row(
      children: [
        _Avatar(initial: user.initial),
        const SizedBox(width: 14),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Flexible(
                    child: Text(
                      user.displayName.isEmpty ? 'Reader' : user.displayName,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(
                        fontFamily: kDisplayFont,
                        fontWeight: FontWeight.w800,
                        fontSize: 19,
                        color: lyc.text,
                      ),
                    ),
                  ),
                  if (user.isOwner) ...[
                    const SizedBox(width: 8),
                    const _OwnerBadge(),
                  ],
                ],
              ),
              if (user.email.isNotEmpty) ...[
                const SizedBox(height: 2),
                Text(
                  user.email,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(fontSize: 12.5, color: lyc.dim),
                ),
              ],
            ],
          ),
        ),
        const SizedBox(width: 8),
        OutlinedButton(
          onPressed: () {
            _name.text = user.displayName;
            setState(() {
              _editing = true;
              _error = null;
            });
          },
          child: const Text('Edit name'),
        ),
      ],
    );
  }

  Widget _editor(Account user) {
    final lyc = context.lyc;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Text(
          'DISPLAY NAME',
          style: TextStyle(
            fontSize: 10,
            fontWeight: FontWeight.w600,
            letterSpacing: 1.4,
            color: lyc.dim,
          ),
        ),
        const SizedBox(height: 8),
        TextField(
          controller: _name,
          autofocus: true,
          maxLength: 40,
          enabled: !_saving,
          textCapitalization: TextCapitalization.words,
          textInputAction: TextInputAction.done,
          onSubmitted: (_) => _save(),
          buildCounter: (_, {required currentLength, required isFocused, maxLength}) => null,
          style: TextStyle(
            fontFamily: kDisplayFont,
            fontWeight: FontWeight.w800,
            fontSize: 19,
            color: lyc.text,
          ),
        ),
        if (_error != null) ...[
          const SizedBox(height: 8),
          Text(_error!, style: TextStyle(fontSize: 12, color: lyc.error)),
        ],
        const SizedBox(height: 12),
        Row(
          children: [
            FilledButton(
              onPressed: _saving ? null : _save,
              child: Text(_saving ? 'Saving…' : 'Save'),
            ),
            const SizedBox(width: 10),
            TextButton(
              onPressed: _saving ? null : () => setState(() => _editing = false),
              child: const Text('Cancel'),
            ),
          ],
        ),
      ],
    );
  }
}

/// The devices list — the one real safeguard in a password-free model.
///
/// A session never expires, so a phone that is lost or lent stays signed in
/// forever unless the person who owns it can see it here and cut it off. That is
/// the entire reason this list exists.
class DevicesSection extends ConsumerWidget {
  const DevicesSection({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final lyc = context.lyc;
    final devices = ref.watch(devicesProvider);

    return devices.when(
      loading: () => Text('Loading…', style: TextStyle(fontSize: 13, color: lyc.dim)),
      error: (e, _) => Text(
        '$e',
        maxLines: 2,
        overflow: TextOverflow.ellipsis,
        style: TextStyle(fontSize: 12.5, color: lyc.error),
      ),
      data: (list) {
        if (list.isEmpty) {
          return Text(
            'No other devices are signed in.',
            style: TextStyle(fontSize: 13, color: lyc.dim),
          );
        }
        return Column(
          children: [
            for (var i = 0; i < list.length; i++) ...[
              if (i > 0) Divider(height: 22, color: lyc.border),
              _DeviceRow(device: list[i]),
            ],
          ],
        );
      },
    );
  }
}

class _DeviceRow extends ConsumerStatefulWidget {
  const _DeviceRow({required this.device});
  final DeviceSession device;

  @override
  ConsumerState<_DeviceRow> createState() => _DeviceRowState();
}

class _DeviceRowState extends ConsumerState<_DeviceRow> {
  bool _busy = false;

  Future<void> _revoke() async {
    final d = widget.device;

    // Cutting off the device you are holding is a sign-out, and must go through
    // the sign-out path: the alternative is revoking our own credential and then
    // trying to re-read a list we can no longer read.
    if (d.current) {
      final confirmed = await showLycSheet<bool>(
        context: context,
        builder: (context) => _SignOutSheet(deviceLabel: d.deviceLabel),
      );
      if (confirmed != true) return;
      try {
        await ref.read(authControllerProvider.notifier).signOut();
      } catch (_) {
        // Already forgotten locally; see AccountSection._signOut.
      }
      return;
    }

    setState(() => _busy = true);
    try {
      await ref.read(lyceumClientProvider).revokeDevice(d.id);
      ref.invalidate(devicesProvider);
    } catch (e) {
      if (!mounted) return;
      setState(() => _busy = false);
      ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text('$e')));
    }
  }

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    final d = widget.device;

    return Row(
      children: [
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Flexible(
                    child: Text(
                      d.deviceLabel.isEmpty ? 'Unnamed device' : d.deviceLabel,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(
                        fontSize: 14,
                        fontWeight: FontWeight.w600,
                        color: lyc.text,
                      ),
                    ),
                  ),
                  if (d.current) ...[
                    const SizedBox(width: 8),
                    _Badge(label: 'This device', color: lyc.brass),
                  ],
                ],
              ),
              const SizedBox(height: 2),
              Text(
                deviceUsedAt(d.lastSeenAt),
                style: TextStyle(fontSize: 12, color: lyc.dim),
              ),
            ],
          ),
        ),
        const SizedBox(width: 8),
        OutlinedButton(
          onPressed: _busy ? null : _revoke,
          child: Text(d.current ? 'Sign out' : 'Revoke'),
        ),
      ],
    );
  }
}

/// Named for what it actually does. "Sign out" on a device that syncs to a server
/// invites the fear that it wipes your place in the book — so the sheet says, in
/// as many words, that it doesn't.
class _SignOutSheet extends StatelessWidget {
  const _SignOutSheet({this.deviceLabel});
  final String? deviceLabel;

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    final label = (deviceLabel ?? '').trim();
    final subject = label.isEmpty ? 'this device' : label;

    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Text('Sign out of Lyceum?', style: Theme.of(context).textTheme.titleLarge),
        const SizedBox(height: 10),
        Text(
          'This signs out $subject only. Your other devices stay signed in. Your '
          'place in every book is saved on the server.',
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
                child: const Text('Sign out'),
              ),
            ),
          ],
        ),
      ],
    );
  }
}

class _Row extends StatelessWidget {
  const _Row({required this.title, required this.hint, required this.action});
  final String title;
  final String hint;
  final Widget action;

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    return Row(
      children: [
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                title,
                style: TextStyle(
                  fontSize: 14,
                  fontWeight: FontWeight.w600,
                  color: lyc.text,
                ),
              ),
              const SizedBox(height: 2),
              Text(
                hint,
                style: TextStyle(fontSize: 12, height: 1.45, color: lyc.dim),
              ),
            ],
          ),
        ),
        const SizedBox(width: 12),
        action,
      ],
    );
  }
}

class _Avatar extends StatelessWidget {
  const _Avatar({required this.initial});
  final String initial;

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    return CircleAvatar(
      radius: 23,
      backgroundColor: lyc.brass,
      child: Text(
        initial,
        style: TextStyle(
          color: lyc.onBrass,
          fontFamily: kDisplayFont,
          fontWeight: FontWeight.w700,
          fontSize: 18,
        ),
      ),
    );
  }
}

class _OwnerBadge extends StatelessWidget {
  const _OwnerBadge();
  @override
  Widget build(BuildContext context) =>
      _Badge(label: 'Owner', color: context.lyc.brass);
}

class _Badge extends StatelessWidget {
  const _Badge({required this.label, required this.color});
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
