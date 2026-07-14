import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../api/server_store.dart';
import '../auth/auth_controller.dart';
import '../features/auth/sign_in_screen.dart';
import '../features/household/household_screen.dart';
import '../features/library/library_screen.dart';
import '../features/reader/reader_screen.dart';
import '../features/scan/scan_screen.dart';
import '../features/settings/settings_screen.dart';

/// Resolve the session once the app knows which server it is talking to.
///
/// Re-runs when the server URL changes, because "who am I?" is a question about
/// a *particular* library — pointing the app at a different one has to ask again.
/// An unconfigured app skips it entirely: there is nobody to ask, and the library
/// screen already shows the connect prompt.
///
/// A failure here is left as a failure: the router does **not** bounce someone to
/// a sign-in screen that also can't reach the server. The library renders its own
/// "Can't reach the library" card instead, which is the truth.
final authBootstrapProvider = FutureProvider<void>((ref) async {
  final url = ref.watch(serverUrlProvider);
  if (url.isEmpty) return;
  await ref.read(authControllerProvider.notifier).load();
});

/// The app's root navigator.
///
/// The signed-out sheet is raised from *above* the router (it has to be able to
/// appear over any screen, including the reader mid-chapter), and a context taken
/// from up there has no Navigator in scope — `showModalBottomSheet` would throw
/// at the exact moment a session ends. So the sheet reaches the navigator through
/// this key instead of through its own context.
final rootNavigatorKey = GlobalKey<NavigatorState>();

/// The screens, mirroring the web routes.
final routerProvider = Provider<GoRouter>((ref) {
  // Bridge Riverpod → go_router. GoRouter re-runs [redirect] when this notifies,
  // which is what turns a session ending into a bounce to the front door.
  final refresh = ValueNotifier<int>(0);
  ref.listen(authControllerProvider, (_, _) => refresh.value++);
  ref.onDispose(refresh.dispose);

  return GoRouter(
    navigatorKey: rootNavigatorKey,
    initialLocation: '/',
    refreshListenable: refresh,
    redirect: (context, state) {
      final auth = ref.read(authControllerProvider);
      final loc = state.matchedLocation;

      // The front door is the only public route.
      if (auth.status == AuthStatus.signedOut) {
        // …but hold position while the "you've been signed out" sheet is up. A
        // 401 arriving mid-chapter should put a calm sheet over the page, not
        // yank the book out from under someone. The bounce happens when they
        // dismiss it.
        if (auth.endedReason != null) return null;
        return loc == '/sign-in' ? null : '/sign-in';
      }
      if (loc == '/sign-in' && auth.isSignedIn) return '/';

      // Household is the owner's alone. A member who somehow lands here is sent
      // home rather than shown a permission error — the UI never offered it.
      if (loc == '/household' && !auth.isOwner) return '/';

      return null;
    },
    routes: [
      GoRoute(path: '/', name: 'library', builder: (_, _) => const LibraryScreen()),
      GoRoute(
        path: '/sign-in',
        name: 'sign-in',
        builder: (_, _) => const SignInScreen(),
      ),
      GoRoute(
        path: '/settings',
        name: 'settings',
        builder: (_, _) => const SettingsScreen(),
      ),
      GoRoute(
        path: '/household',
        name: 'household',
        builder: (_, _) => const HouseholdScreen(),
      ),
      GoRoute(path: '/scan', name: 'scan', builder: (_, _) => const ScanScreen()),
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
