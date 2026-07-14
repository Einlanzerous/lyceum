import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:http/http.dart' as http;

import '../auth/auth_client.dart';
import '../auth/auth_controller.dart';
import '../auth/session_store.dart';
import 'client.dart';
import 'device.dart';
import 'server_store.dart';

/// The raw transport, closed with the provider.
final httpClientProvider = Provider<http.Client>((ref) {
  final client = http.Client();
  ref.onDispose(client.close);
  return client;
});

/// The transport [LyceumClient] actually gets — the raw one wrapped so that
/// every request carries this device's session and every 401 is noticed exactly
/// once, in one place (LYCM-804).
///
/// The token is read through a closure rather than captured, so this provider
/// does *not* rebuild on sign-in: the in-flight requests queued behind the
/// sign-in pick the new credential up on their next send.
final authClientProvider = Provider<AuthClient>((ref) {
  final client = AuthClient(
    ref.watch(httpClientProvider),
    () => ref.read(sessionTokenProvider),
    (reason) => ref.read(authControllerProvider.notifier).sessionEnded(reason),
  );
  // Deliberately NOT ref.onDispose(client.close) — closing it would close the
  // shared inner client, which httpClientProvider already owns.
  return client;
});

/// The typed [LyceumClient], rebuilt whenever the server URL changes.
final lyceumClientProvider = Provider<LyceumClient>((ref) {
  final baseUrl = ref.watch(serverUrlProvider);
  final deviceId = ref.watch(deviceIdProvider);
  return LyceumClient(
    baseUrl: baseUrl,
    deviceId: deviceId,
    httpClient: ref.watch(authClientProvider),
  );
});

/// Headers for an authenticated `Image.network`.
///
/// This is where Flutter has it easier than the browser. The web's entire cover
/// apparatus — a session cookie, plus the object-URL dance in
/// `web/src/api/coverSrc.ts` for the Wails shell — exists only because an `<img>`
/// tag cannot send an `Authorization` header. `Image.network` takes headers, so
/// a gated `/books/{id}/cover` needs nothing more than this map.
///
/// Watched, not read: signing in has to repaint the shelf.
final coverHeadersProvider = Provider<Map<String, String>>((ref) {
  final token = ref.watch(sessionTokenProvider);
  return token.isEmpty ? const {} : {'Authorization': 'Bearer $token'};
});
