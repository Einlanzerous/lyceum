import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../api/api_providers.dart';
import '../../api/models.dart';
import '../../api/server_store.dart';

/// Loads and refreshes the digital shelf. Rebuilds automatically when the
/// server URL changes (via [lyceumClientProvider]). Books are added on the
/// server via its ingestion pipeline, so there is no in-app upload.
class LibraryController extends AsyncNotifier<List<Book>> {
  @override
  Future<List<Book>> build() async {
    if (!ref.watch(hasBackendProvider)) return const [];
    return ref.watch(lyceumClientProvider).listLibrary();
  }

  Future<void> refresh() async {
    state = const AsyncLoading();
    state = await AsyncValue.guard(
      () => ref.read(lyceumClientProvider).listLibrary(),
    );
  }

  /// Mark a book read/unread, updating the shelf optimistically and rolling back
  /// if the server rejects it.
  Future<void> setFinished(int bookId, bool finished) async {
    final current = state.asData?.value;
    if (current == null) return;
    List<Book> patched(bool value) => [
      for (final b in current)
        if (b.id == bookId) b.copyWith(finished: value) else b,
    ];
    state = AsyncData(patched(finished));
    try {
      await ref.read(lyceumClientProvider).setBookFinished(bookId, finished);
    } catch (_) {
      state = AsyncData(patched(!finished));
      rethrow;
    }
  }
}

// retry: (_, _) => null disables Riverpod 3's automatic retry-on-failure for
// this provider. Without it, a failed load (unreachable backend) is silently
// retried every ~12s (one client timeout apart), so the shelf oscillates
// loading -> brief error -> loading and the user just sees a perpetual skeleton
// instead of the _ErrorShelf. Making the failure terminal lets the error card —
// which already has a manual retry button, plus pull-to-refresh — show and stay
// (LYCM-54).
final libraryControllerProvider =
    AsyncNotifierProvider<LibraryController, List<Book>>(
      LibraryController.new,
      retry: (_, _) => null,
    );

/// Grid vs list shelf layout (session-scoped, defaults to grid like the web).
class ViewModeController extends Notifier<bool> {
  @override
  bool build() => true; // true = grid

  void toggle() => state = !state;
}

final gridViewProvider = NotifierProvider<ViewModeController, bool>(
  ViewModeController.new,
);
