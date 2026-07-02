import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:uuid/uuid.dart';

import '../prefs/prefs.dart';

const _kDeviceKey = 'lyceum.device_id';

/// A stable per-install device id (RFC-4122 v4 UUID), generated once and
/// persisted. Sent as `device_id` on every `/sync` call so the server can key
/// reading positions per device (mirrors `web/src/api/device.ts`).
final deviceIdProvider = Provider<String>((ref) {
  final prefs = ref.watch(prefsProvider);
  var id = prefs.getString(_kDeviceKey);
  if (id == null || id.isEmpty) {
    id = const Uuid().v4();
    prefs.setString(_kDeviceKey, id);
  }
  return id;
});
