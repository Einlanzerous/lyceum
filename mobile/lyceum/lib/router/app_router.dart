import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../features/library/library_screen.dart';
import '../features/reader/reader_screen.dart';
import '../features/settings/settings_screen.dart';

/// The three screens, mirroring the web routes: `/` (library),
/// `/reader/:id` (chromeless reader), `/settings`.
final routerProvider = Provider<GoRouter>((ref) {
  return GoRouter(
    initialLocation: '/',
    routes: [
      GoRoute(
        path: '/',
        name: 'library',
        builder: (_, _) => const LibraryScreen(),
      ),
      GoRoute(
        path: '/settings',
        name: 'settings',
        builder: (_, _) => const SettingsScreen(),
      ),
      GoRoute(
        path: '/reader/:id',
        name: 'reader',
        builder: (context, state) {
          final id = int.tryParse(state.pathParameters['id'] ?? '') ?? 0;
          return ReaderScreen(bookId: id);
        },
      ),
    ],
  );
});
