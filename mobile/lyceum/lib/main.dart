import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';

import 'app.dart';
import 'auth/session_store.dart';
import 'prefs/prefs.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  // Edge-to-edge with the system bars visible; screens use SafeArea. Also the
  // baseline the reader restores to after its WebView may go immersive.
  SystemChrome.setEnabledSystemUIMode(
    SystemUiMode.edgeToEdge,
    overlays: SystemUiOverlay.values,
  );

  // Both stores are read before the first frame so every provider downstream can
  // stay synchronous — and so the app never flashes the front door at somebody
  // who is, in fact, already signed in.
  final prefs = await SharedPreferences.getInstance();
  const tokenStore = SecureTokenStore();
  final token = await tokenStore.read();

  runApp(
    ProviderScope(
      overrides: [
        prefsProvider.overrideWithValue(prefs),
        initialSessionTokenProvider.overrideWithValue(token),
      ],
      child: const LyceumApp(),
    ),
  );
}
