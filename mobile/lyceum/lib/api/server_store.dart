import 'package:flutter_riverpod/flutter_riverpod.dart';

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

  Future<void> set(String url) async {
    final normalized = normalizeServerUrl(url);
    final prefs = ref.read(prefsProvider);
    if (normalized.isEmpty) {
      await prefs.remove(_kServerKey);
    } else {
      await prefs.setString(_kServerKey, normalized);
    }
    state = normalized;
  }

  Future<void> clear() => set('');
}

final serverUrlProvider =
    NotifierProvider<ServerUrlController, String>(ServerUrlController.new);

/// True once a server URL is configured (the web `hasBackend()` on native).
final hasBackendProvider =
    Provider<bool>((ref) => ref.watch(serverUrlProvider).isNotEmpty);
