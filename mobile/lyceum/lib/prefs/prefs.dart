import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// The app's single [SharedPreferences] handle. Loaded once in `main()` and
/// injected via a `ProviderScope` override so every persisted store can read
/// and write synchronously (like the web app's `localStorage`).
final prefsProvider = Provider<SharedPreferences>(
  (ref) => throw UnimplementedError(
    'prefsProvider must be overridden in main() with the loaded instance',
  ),
);
