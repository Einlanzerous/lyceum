import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';

/// The session token this device holds (LYCM-804).
///
/// A Lyceum session is a bearer credential with no expiry and no password behind
/// it — whoever holds the string *is* the account. So it lives in the Android
/// keystore, not in SharedPreferences next to the theme choice. The web keeps its
/// token in `localStorage` because a browser offers nothing better; a native app
/// does.
///
/// The key matches the web SPA's (`web/src/api/http.ts`) — not because the two
/// stores are shared (they aren't), but because the reader WebView *is* the web
/// SPA, and the token is injected into its `localStorage` under this name. One
/// spelling keeps that seam obvious.
const kSessionTokenKey = 'lyceum.session_token';

/// Where the session token is kept. An interface, so tests (and any future
/// platform) can swap the keystore out without a platform channel.
abstract interface class TokenStore {
  Future<String> read();
  Future<void> write(String token);
  Future<void> delete();
}

/// The real one: the platform keystore, via `flutter_secure_storage`.
class SecureTokenStore implements TokenStore {
  const SecureTokenStore([this._storage = const FlutterSecureStorage()]);
  final FlutterSecureStorage _storage;

  @override
  Future<String> read() async {
    try {
      return await _storage.read(key: kSessionTokenKey) ?? '';
    } catch (_) {
      // A keystore that won't open — a corrupt entry after a restore-from-backup,
      // a device with a broken provider — must not brick the app. Treat it as
      // signed out; the cost is one extra sign-in, not a dead library.
      return '';
    }
  }

  @override
  Future<void> write(String token) =>
      _storage.write(key: kSessionTokenKey, value: token);

  @override
  Future<void> delete() => _storage.delete(key: kSessionTokenKey);
}

final tokenStoreProvider = Provider<TokenStore>((ref) => const SecureTokenStore());

/// The token read from the keystore at boot, injected via a `ProviderScope`
/// override in `main()`. Reading it is async and every consumer below wants it
/// synchronously — the same shape as `prefsProvider`.
final initialSessionTokenProvider = Provider<String>(
  (ref) => throw UnimplementedError(
    'initialSessionTokenProvider must be overridden in main() with the loaded token',
  ),
);

/// The live session token — `''` when this device holds none.
///
/// Everything downstream keys off this: [AuthClient] attaches it as a bearer
/// header, the reader injects it into the WebView, and the cover images send it
/// as a header of their own.
class SessionTokenController extends Notifier<String> {
  @override
  String build() => ref.watch(initialSessionTokenProvider);

  /// Persist a token — or, with `''`, forget the one we hold.
  Future<void> set(String token) async {
    final store = ref.read(tokenStoreProvider);
    if (token.isEmpty) {
      await store.delete();
    } else {
      await store.write(token);
    }
    state = token;
  }

  Future<void> clear() => set('');
}

final sessionTokenProvider = NotifierProvider<SessionTokenController, String>(
  SessionTokenController.new,
);
