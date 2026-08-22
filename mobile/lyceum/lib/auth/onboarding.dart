import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../api/api_providers.dart';
import '../api/client.dart';
import '../api/server_store.dart';
import '../features/library/library_controller.dart';
import '../router/app_router.dart';
import 'auth_controller.dart';
import 'invite_token.dart';

/// Turning an invite into a working device (LYCM-103).
///
/// A fresh install knows nothing: no server address, no session. One scanned QR
/// is meant to settle both, which makes this two operations that have to happen
/// in a particular order and cannot be collapsed. [Onboarder.connect] runs the
/// whole handoff; [Onboarder.redeem] is the second half on its own, for a key
/// that arrives by paste with the address already known.
///
/// It lives outside the widgets because both front doors need it — the sign-in
/// screen and the Library's connect prompt, which is what a fresh install
/// actually lands on — and because the ordering below is the sort of thing that
/// should be pinned by a test rather than by two screens agreeing.

/// What came of trying to onboard this device.
sealed class OnboardResult {
  const OnboardResult();
}

/// Pointed at the library and signed in. Nothing left to say — the router moves.
final class Onboarded extends OnboardResult {
  const Onboarded();
}

/// The invite named no library and this device has no address for one.
///
/// A bare v1 token or an 8-char pairing code carries no origin, so it can only
/// be redeemed against a server the person names themselves. Not a failure:
/// the manual-entry fallback exists for exactly this.
final class NeedsServerAddress extends OnboardResult {
  const NeedsServerAddress();
}

/// The key was refused (401): wrong, spent, or expired — the server does not
/// say which, by design.
final class InviteRejected extends OnboardResult {
  const InviteRejected();
}

/// Too many tries (429). The pairing-code keyspace is small enough to guess at,
/// so the route is rate-limited; this is a caution, not a bad key.
final class InviteThrottled extends OnboardResult {
  const InviteThrottled();
}

/// [address] did not answer. Never a rejected key — the distinction is the whole
/// point of telling these apart.
final class ServerUnreachable extends OnboardResult {
  const ServerUnreachable(this.address);
  final String address;
}

class Onboarder {
  Onboarder(this._ref);
  final Ref _ref;

  /// Point the app at the library the invite names, then redeem the key.
  ///
  /// The order is forced, and both halves of it are load-bearing:
  ///
  ///  - **Address first.** [ServerUrlController.set] deliberately drops the
  ///    session token, because a session belongs to the library that issued it.
  ///    Redeem first and the credential we just minted would be the one thrown
  ///    away, leaving a device pointed at the right server and signed out of it.
  ///  - **Reachability before *that*.** Committing a dead address costs the
  ///    working one it replaced *and* the session that went with it. So a
  ///    candidate origin is pinged while it is still only a candidate: a QR for
  ///    a server this phone cannot reach leaves the app exactly as it was.
  Future<OnboardResult> connect(
    ScannedInvite invite, {
    String deviceLabel = '',
  }) async {
    final origin = invite.origin;
    final current = _ref.read(serverUrlProvider);

    if (origin == null) {
      // Nothing to point at, and nowhere already pointed: the fallback's turn.
      if (current.isEmpty) return const NeedsServerAddress();
    } else if (origin != current) {
      if (!await _reachable(origin)) return ServerUnreachable(origin);
      await _ref.read(serverUrlProvider.notifier).set(origin);
      await _settle();
    }

    return redeem(invite.token, deviceLabel: deviceLabel);
  }

  /// Redeem a key against the server this device is already pointed at.
  ///
  /// Takes the raw string and routes by shape, so the one sign-in field can go
  /// on accepting either a `lyc_…` invite or a short pairing code without the
  /// person having to say which they hold.
  Future<OnboardResult> redeem(String raw, {String deviceLabel = ''}) async {
    final server = _ref.read(serverUrlProvider);
    if (server.isEmpty) return const NeedsServerAddress();

    try {
      final auth = _ref.read(authControllerProvider.notifier);
      if (looksLikePairingCode(raw)) {
        await auth.signInWithCode(
          normalizePairingCode(raw),
          deviceLabel: deviceLabel,
        );
      } else {
        await auth.signIn(raw, deviceLabel: deviceLabel);
      }
      // The shelf was fetched — and 401'd — before we held a credential, so it
      // needs asking again now that we do.
      _ref.invalidate(libraryControllerProvider);
      return const Onboarded();
    } on ApiException catch (e) {
      if (e.isUnauthorized) return const InviteRejected();
      if (e.isTooManyRequests) return const InviteThrottled();
      return ServerUnreachable(server);
    } catch (_) {
      // Timeout, DNS, connection refused — the server, not the key.
      return ServerUnreachable(server);
    }
  }

  /// `GET {origin}/healthz`, on the raw transport.
  ///
  /// Deliberately not the app's [LyceumClient]: that one is built from the
  /// *configured* address, which is the thing we are still deciding, and it
  /// carries the 401 reaction that would read a probe as a session ending.
  Future<bool> _reachable(String origin) async {
    final client = LyceumClient(
      baseUrl: origin,
      deviceId: '',
      httpClient: _ref.read(httpClientProvider),
    );
    try {
      return await client.ping();
    } catch (_) {
      return false;
    }
  }

  /// Wait out the boot re-resolve that changing the address kicks off.
  ///
  /// [authBootstrapProvider] watches the server URL, so setting it starts a
  /// `GET /auth/me` against the new library. On a server that enforces auth that
  /// request 401s — and its handler clears the session token. Left to race, it
  /// can land *after* the redemption below and throw away the session we just
  /// minted, putting the person back at the front door holding nothing. Letting
  /// it finish first costs one round trip and makes the order deterministic.
  Future<void> _settle() async {
    try {
      await _ref.read(authBootstrapProvider.future);
    } catch (_) {
      // An unreachable server here is the library screen's story to tell, and
      // we pinged the address a moment ago anyway. Never block the redemption.
    }
  }
}

final onboarderProvider = Provider<Onboarder>(Onboarder.new);
