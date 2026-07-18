import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_providers.dart';
import '../api/models.dart';
import '../prefs/prefs.dart';
import 'device_label.dart';
import 'session_store.dart';

/// The legacy per-install display name (LYCM-700), now superseded by the
/// account's `display_name` on the server.
///
/// It is *consumed* on adoption rather than merely read — see
/// [AuthController.adoptLegacyName].
const _kLegacyNameKey = 'lyceum.profile_name';

enum AuthStatus {
  /// Not yet resolved — we haven't asked the server who we are.
  unknown,
  signedIn,
  signedOut,
}

class AuthState {
  const AuthState({
    this.status = AuthStatus.unknown,
    this.user,
    this.sessionEnded = false,
  });

  final AuthStatus status;
  final Account? user;

  /// A session this device was *using* stopped resolving. Drives the "you've been
  /// signed out" sheet, and is cleared once that sheet is dismissed.
  ///
  /// Never set for a device that was simply never signed in — see
  /// [AuthController.unauthorized].
  final bool sessionEnded;

  bool get isSignedIn => status == AuthStatus.signedIn;
  bool get isOwner => user?.isOwner ?? false;
  String get displayName => user?.displayName ?? '';

  /// The avatar letter, generated locally.
  String get initial => user?.initial ?? 'R';

  AuthState copyWith({
    AuthStatus? status,
    Account? user,
    bool? sessionEnded,
    bool clearUser = false,
  }) => AuthState(
    status: status ?? this.status,
    user: clearUser ? null : (user ?? this.user),
    sessionEnded: sessionEnded ?? this.sessionEnded,
  );
}

/// Who this device is signed in as, and whether the server even asks.
class AuthController extends Notifier<AuthState> {
  @override
  AuthState build() => const AuthState();

  /// Resolve the session at boot.
  ///
  /// `GET /auth/me` answers three different questions at once, which is why
  /// there is no separate "is auth on?" endpoint:
  ///
  ///  - **200, token held** → signed in, enforcement on.
  ///  - **200, no token** → enforcement is *off*; the server is serving us as
  ///    the owner, exactly as it did before accounts existed. ([enforcedProvider])
  ///  - **401** → enforcement is on and we are not signed in. Front door.
  ///
  /// Throws if the server is unreachable — the caller (the router) deliberately
  /// lets the route render and fail in its own way rather than bouncing someone
  /// to a sign-in screen that *also* can't reach the server.
  Future<void> load() async {
    final client = ref.read(lyceumClientProvider);
    final auth = ref.read(authClientProvider);

    // Suppressed: a 401 here is the answer to a question, not a session ending.
    final me = await auth.suppressUnauthorized(client.fetchMe);

    if (me == null) {
      // The token we were holding (if any) is dead — the server just said so.
      // Keeping it would leave "signed out" and "holds a credential" both true at
      // once, and [enforcedProvider] reads exactly that distinction.
      await ref.read(sessionTokenProvider.notifier).clear();
      state = const AuthState(status: AuthStatus.signedOut);
      return;
    }
    state = AuthState(status: AuthStatus.signedIn, user: me);
    await adoptLegacyName();
  }

  /// Redeem an invite. A 401 here is *expected input* — a wrong, spent, or
  /// expired key — so it is surfaced to the sign-in screen as a thrown
  /// [ApiException] rather than firing the app-wide signed-out sheet.
  Future<void> signIn(String inviteToken, {String? deviceLabel}) async {
    final client = ref.read(lyceumClientProvider);
    final auth = ref.read(authClientProvider);

    final label = (deviceLabel ?? '').trim().isNotEmpty
        ? deviceLabel!.trim()
        : await inferDeviceLabel();

    final result = await auth.suppressUnauthorized(
      () => client.redeemInvite(inviteToken, deviceLabel: label),
    );

    await ref.read(sessionTokenProvider.notifier).set(result.sessionToken);
    state = AuthState(status: AuthStatus.signedIn, user: result.user);
    await adoptLegacyName();
  }

  /// Redeem a short pairing code instead of a token (LYCM-88). Same outcome as
  /// [signIn]; a 401 (bad/spent/expired) or 429 (rate limited) is thrown to the
  /// sign-in screen to explain, not fired as an app-wide sign-out.
  Future<void> signInWithCode(String code, {String? deviceLabel}) async {
    final client = ref.read(lyceumClientProvider);
    final auth = ref.read(authClientProvider);

    final label = (deviceLabel ?? '').trim().isNotEmpty
        ? deviceLabel!.trim()
        : await inferDeviceLabel();

    final result = await auth.suppressUnauthorized(
      () => client.redeemPairingCode(code, deviceLabel: label),
    );

    await ref.read(sessionTokenProvider.notifier).set(result.sessionToken);
    state = AuthState(status: AuthStatus.signedIn, user: result.user);
    await adoptLegacyName();
  }

  /// Sign this device out. Others keep working.
  ///
  /// The local token is dropped in a `finally`: if the revoke call fails we still
  /// forget the credential, because leaving behind a token that *does* still open
  /// the library is the worst available outcome. The error is not swallowed — the
  /// caller can still say the server wasn't reached.
  ///
  /// The 401 reaction is suppressed for the same reason it is at sign-in: a 401
  /// here means the session was already gone, which is precisely the state we are
  /// trying to reach. Firing "you've been signed out" at somebody who just tapped
  /// Sign out would be a jump scare, not information.
  Future<void> signOut() async {
    final auth = ref.read(authClientProvider);
    try {
      await auth.suppressUnauthorized(ref.read(lyceumClientProvider).signOut);
    } finally {
      await ref.read(sessionTokenProvider.notifier).clear();
      state = const AuthState(status: AuthStatus.signedOut);
    }
  }

  /// `PATCH /auth/me`. The name lives on the server now, so it follows the
  /// person to every device they sign in on.
  Future<void> rename(String name) async {
    final user = await ref.read(lyceumClientProvider).updateDisplayName(name);
    state = state.copyWith(user: user);
  }

  /// The server refused us. [hadToken] says whether we were holding a credential
  /// at the time, and that decides whether anything actually *ended*.
  ///
  /// Holding a token that stops working is an event: a session expired, or was
  /// revoked from another device, or the owner removed the account. The person is
  /// mid-something and deserves the sheet.
  ///
  /// Holding *no* token and being refused is not an event. It means this server
  /// wants a sign-in and this device has never done one — a fresh install that
  /// booted offline and retried once the network came back, or an auth-off server
  /// whose operator has just switched enforcement on. Both are ordinary. Raising
  /// "the library owner removed this account, your reading positions were cleared"
  /// at someone who never had an account is a lie, and an alarming one; they get
  /// the front door instead, quietly, which is exactly what they need.
  ///
  /// Either way it no-ops once already signed out, so the burst of 401s a shelf
  /// render produces (one per cover) is handled exactly once.
  Future<void> unauthorized({required bool hadToken}) async {
    if (state.status == AuthStatus.signedOut) return;
    await ref.read(sessionTokenProvider.notifier).clear();
    state = AuthState(status: AuthStatus.signedOut, sessionEnded: hadToken);
  }

  void clearEnded() => state = state.copyWith(sessionEnded: false);

  /// Carry the pre-accounts local name onto the account, once.
  ///
  /// LYCM-700 kept a display name in SharedPreferences because there were no
  /// accounts to hang one on. Now there are, and someone who has been reading as
  /// "Ada" for a year should not become "Reader" the day they sign in.
  ///
  /// Two rules make this safe:
  ///
  ///  - It only ever **fills a gap** — an empty name, the "Reader" placeholder,
  ///    or the email the server defaults to when an account is created without
  ///    one. A name deliberately set on the server is never overwritten.
  ///  - The local key is **consumed**, not read. Otherwise a stale device-local
  ///    label would keep reverting a rename made months later on another phone.
  ///
  /// It runs on [load] as well as [signIn], so on an auth-off server — where
  /// nobody ever signs in — the old name still folds onto the owner instead of
  /// silently vanishing.
  Future<void> adoptLegacyName() async {
    final user = state.user;
    if (user == null) return;

    final prefs = ref.read(prefsProvider);
    final local = (prefs.getString(_kLegacyNameKey) ?? '').trim();
    if (local.isEmpty) return;

    await prefs.remove(_kLegacyNameKey);
    // SharedPreferences is not reactive, so the greeting on the front door would
    // otherwise keep saying "You've been reading as Ada" to someone who has
    // already signed in, signed out, and come back.
    ref.invalidate(legacyProfileNameProvider);

    final current = user.displayName.trim();
    final isPlaceholder =
        current.isEmpty || current == 'Reader' || current == user.email;
    if (!isPlaceholder || current == local) return;

    try {
      await rename(local);
    } catch (_) {
      // Cosmetic. Never block a sign-in over a display name.
    }
  }
}

final authControllerProvider = NotifierProvider<AuthController, AuthState>(
  AuthController.new,
);

/// Whether this server actually enforces auth.
///
/// There is no server flag to read: with `LYCEUM_AUTH=false` every request is
/// served as the owner, so a client that holds **no token and is still signed
/// in** is talking to an auth-off server. That is the whole test.
///
/// It gates "Sign out" and "Your devices" out of existence there — signing out
/// of a server that issues no invites strands you on a front door you cannot get
/// past, with your own library on the other side of it.
final enforcedProvider = Provider<bool>(
  (ref) => ref.watch(sessionTokenProvider).isNotEmpty,
);

/// The name the returning reader was using before accounts arrived, *without*
/// consuming it — the sign-in screen greets them by it, and they haven't signed
/// in yet.
final legacyProfileNameProvider = Provider<String>(
  (ref) => (ref.watch(prefsProvider).getString(_kLegacyNameKey) ?? '').trim(),
);
