import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:uuid/uuid.dart';

import '../prefs/prefs.dart';

/// The key this id is stored under — the same spelling the web SPA uses in
/// `localStorage`, because the reader WebView *is* the web SPA and has the
/// native id injected into it under this name (see `reader_screen.dart`).
const kDeviceIdKey = 'lyceum.device_id';

/// A stable per-install device id (RFC-4122 v4 UUID), generated once and
/// persisted. Sent as `device_id` on every `/sync` call so the server can key
/// reading positions per device (mirrors `web/src/api/device.ts`).
final deviceIdProvider = Provider<String>((ref) {
  final prefs = ref.watch(prefsProvider);
  var id = prefs.getString(kDeviceIdKey);
  if (id == null || id.isEmpty) {
    id = const Uuid().v4();
    prefs.setString(kDeviceIdKey, id);
  }
  return id;
});
