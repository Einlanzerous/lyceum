import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:http/http.dart' as http;

import 'client.dart';
import 'device.dart';
import 'server_store.dart';

/// A shared [http.Client], closed with the provider.
final httpClientProvider = Provider<http.Client>((ref) {
  final client = http.Client();
  ref.onDispose(client.close);
  return client;
});

/// The typed [LyceumClient], rebuilt whenever the server URL changes (like
/// Argosy's `apiClientProvider` watching `baseUrlProvider`).
final lyceumClientProvider = Provider<LyceumClient>((ref) {
  final baseUrl = ref.watch(serverUrlProvider);
  final deviceId = ref.watch(deviceIdProvider);
  return LyceumClient(
    baseUrl: baseUrl,
    deviceId: deviceId,
    httpClient: ref.watch(httpClientProvider),
  );
});
