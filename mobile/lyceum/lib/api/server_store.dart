import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../auth/session_store.dart';
import '../prefs/prefs.dart';

/// Trim and strip trailing slashes — identical to the web
/// `normalizeServerUrl` in `api/base.ts`.
String normalizeServerUrl(String url) =>
    url.trim().replaceAll(RegExp(r'/+$'), '');

const _kServerKey = 'lyceum.server_url';

/// Optional compile-time default for dev, e.g.
/// `flutter run --dart-define=LYCEUM_BASE_URL=http://10.0.0.20:8080`.
const _kCompileDefault = String.fromEnvironment('LYCEUM_BASE_URL');

/// The configured backend base URL (normalized, no trailing slash). Empty
/// string means "not configured yet" — the app then shows the connect prompt.
/// This is the Flutter analogue of the web base-URL seam; on Android there is
/// no same-origin option, so a server URL is always required.
class ServerUrlController extends Notifier<String> {
  @override
  String build() {
    final saved = ref.watch(prefsProvider).getString(_kServerKey);
    if (saved != null && saved.isNotEmpty) return saved;
    return normalizeServerUrl(_kCompileDefault);
  }

  /// Point the app at a library.
  ///
  /// **A session belongs to the library that issued it.** Pointing the app
  /// somewhere else therefore drops the token: it is meaningless to the new
  /// server, and keeping it does active harm — [enforcedProvider] reads "we hold
  /// a token" as "this server enforces auth", so a leftover credential from the
  /// old library makes a brand-new *auth-off* one look enforced. Settings would
  /// then offer a Sign out button which, tapped, bounces the reader to a front
  /// door that issues no invites and cannot be got past, with their own shelf on
  /// the other side of it.
  Future<void> set(String url) async {
    final normalized = normalizeServerUrl(url);
    if (normalized == state) return; // re-saving the same address is a no-op

    final prefs = ref.read(prefsProvider);
    if (normalized.isEmpty) {
      await prefs.remove(_kServerKey);
    } else {
      await prefs.setString(_kServerKey, normalized);
    }
    await ref.read(sessionTokenProvider.notifier).clear();
    state = normalized;
  }

  Future<void> clear() => set('');
}

final serverUrlProvider =
    NotifierProvider<ServerUrlController, String>(ServerUrlController.new);

/// True once a server URL is configured (the web `hasBackend()` on native).
final hasBackendProvider =
    Provider<bool>((ref) => ref.watch(serverUrlProvider).isNotEmpty);
