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

  /// Remove a book from the library for good (LYCM-109). The tile is dropped
  /// optimistically and put back at its old place if the server refuses, so a
  /// failed delete doesn't silently lose the book from the shelf.
  Future<void> remove(int bookId) async {
    final current = state.asData?.value;
    if (current == null) return;
    final index = current.indexWhere((b) => b.id == bookId);
    if (index == -1) return;
    final removed = current[index];
    state = AsyncData([
      for (final b in current)
        if (b.id != bookId) b,
    ]);
    try {
      await ref.read(lyceumClientProvider).deleteBook(bookId);
    } catch (_) {
      // Restore just this book, into whatever the shelf looks like now — a
      // refresh or another removal may have landed while the request was in
      // flight, and putting the whole pre-delete snapshot back would revert it.
      final now = state.asData?.value ?? const <Book>[];
      if (!now.any((b) => b.id == bookId)) {
        state = AsyncData(
          [...now]..insert(index.clamp(0, now.length), removed),
        );
      }
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
