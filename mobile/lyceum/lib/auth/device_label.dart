import 'dart:io' show Platform;

import 'package:device_info_plus/device_info_plus.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

/// A human-readable name for this device (LYCM-804).
///
/// The sign-in screen shows this pre-filled with a "change" affordance rather
/// than asking for it. Making the front door a two-field form to populate a list
/// most people will look at once is a bad trade; a guess they can correct keeps
/// the door single-field *and* keeps the devices list meaningful — the whole
/// reason that list exists is to recognise a phone you lost and cut it off.
///
/// This is a *label*, not an identity: `lyceum.device_id` remains what /sync
/// keys on. Two people can both call their phone "Pixel"; nothing breaks.
///
/// Mirrors `inferDeviceLabel()` in `web/src/api/device.ts`, which reads a user
/// agent. Android has the real thing.
Future<String> inferDeviceLabel([DeviceInfoPlugin? plugin]) async {
  if (!Platform.isAndroid) return 'This device';
  try {
    final info = await (plugin ?? DeviceInfoPlugin()).androidInfo;
    return composeDeviceLabel(
      manufacturer: info.manufacturer,
      model: info.model,
    );
  } catch (_) {
    return 'This device';
  }
}

/// Join a manufacturer and model into a label, without stuttering.
///
/// Google reports model "Pixel 8" (→ "Google Pixel 8"), but Samsung reports
/// "SM-G991B", which is meaningless without "Samsung" in front of it. So the
/// manufacturer leads — except where the model already carries it ("OnePlus
/// 12" would otherwise become "OnePlus OnePlus 12").
String composeDeviceLabel({
  required String manufacturer,
  required String model,
}) {
  final make = manufacturer.trim();
  final name = model.trim();
  if (name.isEmpty) return make.isEmpty ? 'Android device' : make;
  if (make.isEmpty) return name;
  if (name.toLowerCase().startsWith(make.toLowerCase())) return name;
  return '${make[0].toUpperCase()}${make.substring(1)} $name';
}

/// The inferred label, resolved once per app run.
final deviceLabelProvider = FutureProvider<String>((ref) => inferDeviceLabel());
