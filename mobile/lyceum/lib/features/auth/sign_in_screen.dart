import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../api/server_store.dart';
import '../../auth/auth_controller.dart';
import '../../auth/device_label.dart';
import '../../auth/invite_token.dart';
import '../../auth/onboarding.dart';
import '../../features/library/library_controller.dart';
import '../../features/settings/server_settings.dart';
import 'scan_onboarding.dart';
import '../../theme/lyceum_colors.dart';
import '../../theme/lyceum_theme.dart';
import '../../widgets/brand_mark.dart';
import '../../widgets/lyc_sheet.dart';

/// The front door (LYCM-804, LYCM-103).
///
/// One thing to do: scan the invite. There are no passwords here — you redeem a
/// key once and this device keeps a durable session. The QR is the primary path
/// because it is the only one that needs nothing typed: it carries the library's
/// address as well as the key, which is the whole of onboarding a fresh install.
/// The field below it, and the server address below that, are the fallbacks for
/// what a QR cannot carry — a bare v1 token, a short pairing code, a LAN box.
/// Everything else on the screen exists to make the ways it can fail legible.
class SignInScreen extends ConsumerStatefulWidget {
  const SignInScreen({super.key});

  @override
  ConsumerState<SignInScreen> createState() => _SignInScreenState();
}

/// How the last attempt failed. The distinction matters more than it looks:
/// a rejected key is *your* problem to fix, an unreachable server is not, and
/// showing the red "bad key" banner for a flat network is a small lie that
/// sends people hunting for a new invite they don't need.
///
/// [needsServer] is the one that isn't a failure at all: a key that names no
/// library, on a device that has none. It asks for the fallback rather than
/// apologising.
enum _Failure { rejected, throttled, unreachable, needsServer }

class _SignInScreenState extends ConsumerState<SignInScreen> {
  final _invite = TextEditingController();
  final _label = TextEditingController();
  final _inviteFocus = FocusNode();

  String _deviceLabel = '';
  bool _editingLabel = false;
  bool _submitting = false;
  // Scanning is a kind of submitting — it ends in a redemption — but the two
  // want the spinner in different places, so the screen tracks which it is in.
  bool _scanning = false;
  bool _showServerSettings = false;
  _Failure? _failure;

  /// The invite the camera last handed us, kept only so a failure can be
  /// retried against the library it named. Without it, Retry after an
  /// unreachable scan would quietly re-send the scanned key to the *old*
  /// server — which answers 401, and turns "your network is down" into "your
  /// key is bad". Cleared the moment the field is edited by hand: a typed key
  /// is a new intent.
  ScannedInvite? _scanned;

  /// The address that didn't answer, when we know which one it was.
  String? _unreachable;

  @override
  void initState() {
    super.initState();
    _invite.addListener(_onInviteChanged);
    inferDeviceLabel().then((label) {
      if (mounted) setState(() => _deviceLabel = label);
    });
  }

  @override
  void dispose() {
    _invite.removeListener(_onInviteChanged);
    _invite.dispose();
    _label.dispose();
    _inviteFocus.dispose();
    super.dispose();
  }

  /// Take whatever they typed and stop editing. An empty field falls back to the
  /// inferred label rather than sending a blank one — the devices list is only
  /// useful if every row says *something*.
  void _commitLabel() {
    if (!_editingLabel) return;
    final typed = _label.text.trim();
    setState(() {
      if (typed.isNotEmpty) _deviceLabel = typed;
      _editingLabel = false;
    });
  }

  void _onInviteChanged() {
    _scanned = null;
    // Clear a rejection or throttle the moment they start fixing it — a banner
    // that outlives the key it was about is just nagging.
    if (_failure == _Failure.rejected || _failure == _Failure.throttled) {
      setState(() => _failure = null);
    } else {
      setState(() {}); // the button and the footnote both track emptiness
    }
  }

  bool get _hasToken => _invite.text.trim().isNotEmpty;

  Future<void> _paste() async {
    final data = await Clipboard.getData(Clipboard.kTextPlain);
    final text = data?.text ?? '';
    if (text.isEmpty || !mounted) return;
    _invite.text = text.trim();
    _invite.selection = TextSelection.collapsed(offset: _invite.text.length);
  }

  /// Scan the invite QR a signed-in device is showing (LYCM-88, LYCM-103).
  ///
  /// The scanner hands back the address as well as the key, and [scanAndOnboard]
  /// applies them in the order that survives: point the app at the library, then
  /// redeem against it. Scanning is an unambiguous "yes, this one", so nothing
  /// waits for a second tap.
  Future<void> _scan() async {
    if (_submitting) return;
    setState(() {
      _submitting = true;
      _scanning = true;
      _failure = null;
    });
    try {
      final result = await scanAndOnboard(
        context,
        ref,
        deviceLabel: _deviceLabel,
        // The key lands in the field before anything can refuse it, so a
        // rejection leaves something to look at — and Try again something to
        // send.
        onScanned: (invite) {
          _invite.text = invite.token;
          _invite.selection = TextSelection.collapsed(
            offset: _invite.text.length,
          );
          // After the field, whose listener clears it.
          _scanned = invite;
        },
      );
      if (!mounted || result == null) return;
      _apply(result);
    } finally {
      if (mounted) {
        setState(() {
          _submitting = false;
          _scanning = false;
        });
      }
    }
  }

  /// Try the last attempt again, whichever it was. A scanned invite goes back
  /// through [Onboarder.connect] — address and all — rather than being re-sent
  /// to whatever server this device happens to still be pointed at.
  Future<void> _retry() async {
    final scanned = _scanned;
    if (scanned == null) return _submit();
    if (_submitting) return;
    setState(() {
      _submitting = true;
      _failure = null;
    });
    final result = await ref
        .read(onboarderProvider)
        .connect(scanned, deviceLabel: _deviceLabel);
    if (!mounted) return;
    setState(() => _submitting = false);
    _apply(result);
  }

  Future<void> _submit() async {
    if (!_hasToken || _submitting) return;
    FocusScope.of(context).unfocus();
    setState(() {
      _submitting = true;
      _failure = null;
    });
    // The one field takes either a full invite or the short pairing code
    // (LYCM-88); the onboarder routes by shape, so the person never has to say
    // which they have.
    final result = await ref
        .read(onboarderProvider)
        .redeem(_invite.text, deviceLabel: _deviceLabel);
    if (!mounted) return;
    setState(() => _submitting = false);
    _apply(result);
  }

  /// Render an outcome. A success needs nothing: the router's redirect carries
  /// us to the library the moment the auth state flips.
  void _apply(OnboardResult result) {
    setState(() {
      _unreachable = result is ServerUnreachable ? result.address : null;
      _failure = switch (result) {
        Onboarded() => null,
        InviteRejected() => _Failure.rejected,
        InviteThrottled() => _Failure.throttled,
        ServerUnreachable() => _Failure.unreachable,
        NeedsServerAddress() => _Failure.needsServer,
      };
      // A key that names no library can only be redeemed against an address the
      // person gives us, so put that field in front of them rather than leaving
      // them to go looking for it.
      if (_failure == _Failure.needsServer) _showServerSettings = true;
    });
  }

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    final hasBackend = ref.watch(hasBackendProvider);
    final returningName = ref.watch(legacyProfileNameProvider);
    final upgrade = returningName.isNotEmpty;

    return Scaffold(
      backgroundColor: lyc.bg,
      body: SafeArea(
        child: Center(
          child: SingleChildScrollView(
            padding: const EdgeInsets.fromLTRB(20, 24, 20, 32),
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 440),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  const Center(child: BrandMark()),
                  const SizedBox(height: 26),

                  if (!hasBackend)
                    _NoServerCard(
                      onScan: _scan,
                      scanning: _scanning,
                      failure: _failure,
                      showManual: _showServerSettings,
                      onToggleManual: () => setState(
                        () => _showServerSettings = !_showServerSettings,
                      ),
                      onSaved: () {
                        ref.invalidate(libraryControllerProvider);
                        setState(() {
                          _failure = null;
                          _showServerSettings = false;
                        });
                      },
                    )
                  else ...[
                    _Card(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.stretch,
                        children: [
                          if (_failure == _Failure.rejected)
                            const _RejectedBanner()
                          else if (_failure == _Failure.throttled)
                            const _ThrottledBanner()
                          else
                            _Headline(
                              upgrade: upgrade,
                              returningName: returningName,
                            ),
                          const SizedBox(height: 22),
                          // The one tap that needs nothing typed goes first and
                          // wears the filled button; the key field below it is
                          // for what a QR can't carry.
                          ScanInviteButton(
                            onPressed: _submitting ? null : _scan,
                            busy: _scanning,
                          ),
                          const SizedBox(height: 16),
                          const _Or(),
                          const SizedBox(height: 16),
                          _inviteField(upgrade: upgrade),
                          if (_hasToken && _failure == null) ...[
                            const SizedBox(height: 10),
                            _deviceRow(),
                          ],
                          const SizedBox(height: 16),
                          _submitButton(upgrade: upgrade),
                          const SizedBox(height: 14),
                          _Foot(
                            rejected: _failure == _Failure.rejected,
                            upgrade: upgrade,
                            hasToken: _hasToken,
                          ),
                          const SizedBox(height: 6),
                          // Always reachable, not just when the server is
                          // unreachable. Sign-in is the *only* public route, so
                          // Settings — and with it the server address — cannot be
                          // opened from here. Someone who points the app at a
                          // second, perfectly reachable library and has no invite
                          // for it would otherwise be sealed out of their own
                          // shelf, with the only escape being to clear app data.
                          Center(
                            child: TextButton(
                              onPressed: () => setState(
                                () =>
                                    _showServerSettings = !_showServerSettings,
                              ),
                              child: Text(
                                _showServerSettings
                                    ? 'Hide server address'
                                    : 'Change server address',
                                style: TextStyle(fontSize: 12, color: lyc.dim),
                              ),
                            ),
                          ),
                        ],
                      ),
                    ),
                    if (_failure == _Failure.unreachable) ...[
                      const SizedBox(height: 16),
                      _UnreachableCard(
                        address: _unreachable,
                        onRetry: _retry,
                        onServerAddress: () => setState(
                          () => _showServerSettings = !_showServerSettings,
                        ),
                      ),
                    ],
                    if (_showServerSettings) ...[
                      const SizedBox(height: 16),
                      _Card(
                        child: ServerSettings(
                          onSaved: () {
                            ref.invalidate(libraryControllerProvider);
                            setState(() {
                              _failure = null;
                              _showServerSettings = false;
                            });
                          },
                        ),
                      ),
                    ],
                  ],
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }

  Widget _inviteField({required bool upgrade}) {
    final lyc = context.lyc;
    final rejected = _failure == _Failure.rejected;
    final label = upgrade && !rejected
        ? 'Paste your invite to continue'
        : 'Or paste a key';

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Text(
          label.toUpperCase(),
          style: TextStyle(
            fontSize: 10,
            fontWeight: FontWeight.w600,
            letterSpacing: 1.4,
            color: lyc.dim,
          ),
        ),
        const SizedBox(height: 7),
        TextField(
          controller: _invite,
          focusNode: _inviteFocus,
          enabled: !_submitting,
          autocorrect: false,
          enableSuggestions: false,
          textInputAction: TextInputAction.go,
          onSubmitted: (_) => _submit(),
          style: TextStyle(
            fontFamily: 'monospace',
            fontSize: 14,
            color: rejected ? lyc.error : lyc.text,
          ),
          decoration: InputDecoration(
            hintText: 'lyc_… or code',
            hintStyle: TextStyle(
              fontFamily: 'monospace',
              fontSize: 14,
              color: lyc.dimmer,
            ),
            // The paste affordance the design asks for, sitting on the field
            // itself rather than in a toolbar — an invite always arrives via the
            // clipboard, out of a chat message or a terminal, and long-press →
            // "Paste" is a lot of ceremony for the one thing anyone does here.
            // No green tick next to a red banner: the key that was just refused
            // is not "good to go", and saying so twice in one field is the kind
            // of small incoherence that makes people distrust the rest.
            // Scan used to sit here beside paste. It is a primary button now, so
            // this is the clipboard alone — the fallback for a key that arrived
            // as text rather than as a code to point a camera at.
            suffixIcon: rejected
                ? null
                : _hasToken
                ? Icon(Icons.check_rounded, size: 18, color: lyc.success)
                : IconButton(
                    onPressed: _submitting ? null : _paste,
                    icon: Icon(
                      Icons.content_paste_rounded,
                      size: 18,
                      color: lyc.brass,
                    ),
                    tooltip: 'Paste',
                  ),
            enabledBorder: OutlineInputBorder(
              borderRadius: BorderRadius.circular(LycRadii.card),
              borderSide: BorderSide(
                color: rejected
                    ? lyc.error.withValues(alpha: 0.55)
                    : lyc.borderStrong,
              ),
            ),
            focusedBorder: OutlineInputBorder(
              borderRadius: BorderRadius.circular(LycRadii.card),
              borderSide: BorderSide(
                color: rejected ? lyc.error : lyc.brass,
                width: 1.5,
              ),
            ),
          ),
        ),
      ],
    );
  }

  /// "This device · **Pixel 8** · change" — inferred, not asked. A two-field
  /// front door to populate a list most people look at once is a bad trade.
  Widget _deviceRow() {
    final lyc = context.lyc;

    if (_editingLabel) {
      return TextField(
        controller: _label,
        autofocus: true,
        maxLength: 40,
        style: TextStyle(fontSize: 13, color: lyc.text),
        buildCounter:
            (_, {required currentLength, required isFocused, maxLength}) =>
                null,
        decoration: const InputDecoration(
          isDense: true,
          labelText: 'Device name',
        ),
        onSubmitted: (_) => _commitLabel(),
        // Committed on the way out, not discarded: someone who renames their
        // phone and then taps the invite field has still renamed their phone.
        onTapOutside: (_) => _commitLabel(),
      );
    }

    return Row(
      children: [
        Icon(Icons.smartphone_rounded, size: 14, color: lyc.dim),
        const SizedBox(width: 7),
        Flexible(
          child: Text.rich(
            TextSpan(
              children: [
                const TextSpan(text: 'This device · '),
                TextSpan(
                  text: _deviceLabel.isEmpty ? 'This device' : _deviceLabel,
                  style: TextStyle(
                    fontWeight: FontWeight.w700,
                    color: lyc.reading,
                  ),
                ),
                const TextSpan(text: ' · '),
              ],
            ),
            overflow: TextOverflow.ellipsis,
            style: TextStyle(fontSize: 11.5, color: lyc.dim),
          ),
        ),
        GestureDetector(
          onTap: () => setState(() {
            _label.text = _deviceLabel;
            _editingLabel = true;
          }),
          child: Text(
            'change',
            style: TextStyle(
              fontSize: 11.5,
              fontWeight: FontWeight.w600,
              color: lyc.brass,
            ),
          ),
        ),
      ],
    );
  }

  Widget _submitButton({required bool upgrade}) {
    final label = switch ((_submitting && !_scanning, _failure, upgrade)) {
      (true, _, _) => 'Unlocking…',
      (_, _Failure.rejected, _) => 'Try again',
      (_, _, true) => 'Keep my library',
      _ => 'Unlock the library',
    };

    // Outlined, not filled: Scan invite above it is the primary action now, and
    // two filled buttons in one card would say the choice doesn't matter.
    final busy = _submitting && !_scanning;
    return OutlinedButton(
      onPressed: _hasToken && !_submitting ? _submit : null,
      child: busy
          ? Row(
              mainAxisAlignment: MainAxisAlignment.center,
              children: [
                SizedBox(
                  width: 14,
                  height: 14,
                  child: CircularProgressIndicator(
                    strokeWidth: 2,
                    color: context.lyc.brass,
                  ),
                ),
                const SizedBox(width: 9),
                Text(label),
              ],
            )
          : Text(label),
    );
  }
}

/// The 401 banner. One status code covers a key that is wrong, a key that is
/// spent, and a key that has expired — the server cannot tell them apart, by
/// design, so the copy names all three rather than guessing at one.
class _RejectedBanner extends StatelessWidget {
  const _RejectedBanner();

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    return LycNotice(
      tone: LycTone.error,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            "That key didn't work.",
            style: TextStyle(
              fontSize: 13,
              fontWeight: FontWeight.w800,
              color: lyc.error,
            ),
          ),
          const SizedBox(height: 4),
          Text(
            'Invites and codes work once — this one may be spent, expired, or '
            'mistyped. We can\'t tell which. Ask for a fresh one, or check you '
            'copied the whole thing.',
            style: TextStyle(fontSize: 12.5, height: 1.5, color: lyc.muted),
          ),
        ],
      ),
    );
  }
}

/// The 429 banner. Not a bad key — the pairing-code path is rate-limited so its
/// small keyspace can't be hammered, and this is a caution, not an error.
class _ThrottledBanner extends StatelessWidget {
  const _ThrottledBanner();

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    return LycNotice(
      tone: LycTone.warning,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            'Too many tries.',
            style: TextStyle(
              fontSize: 13,
              fontWeight: FontWeight.w800,
              color: lyc.brassBright,
            ),
          ),
          const SizedBox(height: 4),
          Text(
            'For safety we limit how fast codes can be tried. Wait a minute, then '
            'enter it again — or paste the full invite instead.',
            style: TextStyle(fontSize: 12.5, height: 1.5, color: lyc.muted),
          ),
        ],
      ),
    );
  }
}

class _Headline extends StatelessWidget {
  const _Headline({required this.upgrade, required this.returningName});
  final bool upgrade;
  final String returningName;

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Text(
          upgrade ? 'WELCOME BACK' : 'THE READING ROOM',
          textAlign: TextAlign.center,
          style: TextStyle(
            fontFamily: kDisplayFont,
            fontSize: 11,
            fontWeight: FontWeight.w700,
            letterSpacing: 2.4,
            color: lyc.brass,
          ),
        ),
        const SizedBox(height: 10),
        Text(
          upgrade
              ? "You've been reading as $returningName."
              : "You've been handed a key.",
          textAlign: TextAlign.center,
          style: TextStyle(
            fontFamily: kDisplayFont,
            fontSize: 24,
            height: 1.15,
            fontWeight: FontWeight.w800,
            color: lyc.text,
          ),
        ),
        const SizedBox(height: 12),
        if (upgrade) ...[
          Text.rich(
            TextSpan(
              children: [
                const TextSpan(
                  text:
                      'This library just turned on accounts. Sign in once on '
                      'this device to keep going — ',
                ),
                TextSpan(
                  text:
                      'your shelf and your place in every book come with you.',
                  style: TextStyle(
                    fontWeight: FontWeight.w700,
                    color: lyc.reading,
                  ),
                ),
              ],
            ),
            textAlign: TextAlign.center,
            style: TextStyle(fontSize: 13.5, height: 1.55, color: lyc.muted),
          ),
          const SizedBox(height: 16),
          _Promise(
            '“$returningName” becomes your account name — you can change it any time.',
          ),
          const SizedBox(height: 8),
          const _Promise(
            'Every bookmark and reading position stays exactly where it is.',
          ),
        ] else
          Text(
            'Point the camera at the QR on your invite — that is the whole of '
            'it. A pasted key or a short code works too.',
            textAlign: TextAlign.center,
            style: TextStyle(fontSize: 13.5, height: 1.55, color: lyc.muted),
          ),
      ],
    );
  }
}

class _Promise extends StatelessWidget {
  const _Promise(this.text);
  final String text;

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Icon(Icons.check_rounded, size: 15, color: lyc.success),
        const SizedBox(width: 9),
        Expanded(
          child: Text(
            text,
            style: TextStyle(fontSize: 12.5, height: 1.5, color: lyc.muted),
          ),
        ),
      ],
    );
  }
}

/// A network failure is not a rejected key, and must never wear the red banner:
/// nobody should go hunting for a fresh invite because their Wi-Fi dropped.
class _UnreachableCard extends StatelessWidget {
  const _UnreachableCard({
    required this.onRetry,
    required this.onServerAddress,
    this.address,
  });
  final VoidCallback onRetry;
  final VoidCallback onServerAddress;

  /// Which library didn't answer. Worth printing when a scan supplied it: the
  /// address came off a QR, so it is the one thing on screen the person has
  /// never seen and cannot check.
  final String? address;

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    return _Card(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Icon(Icons.wifi_off_rounded, size: 20, color: lyc.muted),
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(
                      "Can't reach this library.",
                      style: TextStyle(
                        fontSize: 14,
                        fontWeight: FontWeight.w800,
                        color: lyc.text,
                      ),
                    ),
                    const SizedBox(height: 3),
                    Text(
                      "The server may be off, or this device isn't on the same "
                      'network.',
                      style: TextStyle(
                        fontSize: 12.5,
                        height: 1.5,
                        color: lyc.muted,
                      ),
                    ),
                    if (address != null) ...[
                      const SizedBox(height: 6),
                      Text(
                        address!,
                        style: TextStyle(
                          fontFamily: 'monospace',
                          fontSize: 12,
                          color: lyc.dim,
                        ),
                      ),
                    ],
                  ],
                ),
              ),
            ],
          ),
          const SizedBox(height: 16),
          Row(
            children: [
              Expanded(
                child: FilledButton(
                  onPressed: onRetry,
                  child: const Text('Retry'),
                ),
              ),
              const SizedBox(width: 10),
              Expanded(
                child: OutlinedButton(
                  onPressed: onServerAddress,
                  child: const Text('Server address'),
                ),
              ),
            ],
          ),
        ],
      ),
    );
  }
}

/// A rule with "or" set into it, marking where the easy path ends and the
/// fallbacks begin.
class _Or extends StatelessWidget {
  const _Or();

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    final rule = Expanded(child: Divider(height: 1, color: lyc.border));
    return Row(
      children: [
        rule,
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: 12),
          child: Text(
            'or',
            style: TextStyle(
              fontSize: 11,
              fontWeight: FontWeight.w600,
              letterSpacing: 0.6,
              color: lyc.dim,
            ),
          ),
        ),
        rule,
      ],
    );
  }
}

/// What a fresh install sees: no address, no session, nothing to type (LYCM-103).
///
/// This card used to be a URL field and an instruction to go and find the
/// address. It asked the wrong person: on the direct path the address is a
/// Cloudflare hostname nobody has memorised, and the one device that knows it is
/// the one showing the QR. So the QR answers both questions at once, and the
/// field it replaced stays one tap away — a bare v1 token, an 8-char pairing
/// code, and a LAN or dev box all still arrive without an address attached.
class _NoServerCard extends StatelessWidget {
  const _NoServerCard({
    required this.onSaved,
    required this.onScan,
    required this.scanning,
    required this.failure,
    required this.showManual,
    required this.onToggleManual,
  });

  final VoidCallback onSaved;
  final VoidCallback onScan;
  final bool scanning;
  final _Failure? failure;
  final bool showManual;
  final VoidCallback onToggleManual;

  /// What went wrong last time, in this card's terms. `needsServer` is the
  /// interesting one: the scan worked, the key is fine, and the only thing
  /// missing is the address — which the field just below now wants.
  String? get _note => switch (failure) {
    _Failure.needsServer =>
      "That code doesn't say which library it belongs to. Enter the address "
          'below, then paste the key.',
    _Failure.unreachable =>
      "Couldn't reach the library in that invite. Check this phone's "
          'connection, and that the address it names is one it can see.',
    _Failure.rejected =>
      "That key didn't work — it may be spent, expired, or already used. Ask "
          'for a fresh one.',
    _Failure.throttled => 'Too many tries. Wait a minute, then scan again.',
    null => null,
  };

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    final note = _note;

    return _Card(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(
            'Scan the invite you were sent.',
            style: Theme.of(context).textTheme.titleMedium,
          ),
          const SizedBox(height: 8),
          Text(
            'The QR carries both halves: which library to talk to, and the key '
            'that opens it. Nothing to type.',
            style: TextStyle(fontSize: 13, height: 1.5, color: lyc.muted),
          ),
          const SizedBox(height: 18),
          ScanInviteButton(onPressed: scanning ? null : onScan, busy: scanning),
          if (note != null) ...[
            const SizedBox(height: 14),
            LycNotice(
              tone: failure == _Failure.needsServer
                  ? LycTone.warning
                  : LycTone.error,
              child: Text(
                note,
                style: TextStyle(fontSize: 12.5, height: 1.5, color: lyc.muted),
              ),
            ),
          ],
          const SizedBox(height: 6),
          Center(
            child: TextButton(
              onPressed: onToggleManual,
              child: Text(
                showManual
                    ? 'Hide server address'
                    : 'Enter a server address instead',
                style: TextStyle(fontSize: 12, color: lyc.dim),
              ),
            ),
          ),
          if (showManual) ...[
            const SizedBox(height: 6),
            ServerSettings(onSaved: onSaved),
          ],
        ],
      ),
    );
  }
}

class _Foot extends StatelessWidget {
  const _Foot({
    required this.rejected,
    required this.upgrade,
    required this.hasToken,
  });

  final bool rejected;
  final bool upgrade;
  final bool hasToken;

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    final style = TextStyle(fontSize: 12, height: 1.5, color: lyc.dim);

    if (rejected) {
      return Text.rich(
        TextSpan(
          children: [
            const TextSpan(
              text:
                  'Still stuck? Whoever runs the library can issue another, or run ',
            ),
            TextSpan(
              text: 'lyceum mint-token',
              style: TextStyle(fontFamily: 'monospace', color: lyc.muted),
            ),
            const TextSpan(text: ' on the box.'),
          ],
        ),
        textAlign: TextAlign.center,
        style: style,
      );
    }

    final text = switch ((upgrade, hasToken)) {
      (true, _) =>
        'Owner? Your invite is in the server log on first boot. Everyone else: '
            'ask the owner.',
      (false, true) => 'Whitespace and line breaks are fine — we clean it up.',
      _ =>
        'No invite? Ask whoever runs this library for one. There are no '
            'passwords here.',
    };
    return Text(text, textAlign: TextAlign.center, style: style);
  }
}

class _Card extends StatelessWidget {
  const _Card({required this.child});
  final Widget child;

  @override
  Widget build(BuildContext context) {
    final lyc = context.lyc;
    return Container(
      padding: const EdgeInsets.fromLTRB(22, 24, 22, 22),
      decoration: BoxDecoration(
        color: lyc.surface,
        borderRadius: BorderRadius.circular(LycRadii.lg),
        border: Border.all(color: lyc.border),
      ),
      child: child,
    );
  }
}
