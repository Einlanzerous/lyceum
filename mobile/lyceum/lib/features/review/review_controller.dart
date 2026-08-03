import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../api/api_providers.dart';
import '../../api/client.dart';
import '../../api/models.dart';
import '../../api/server_store.dart';

/// The ingest-QC review queue (LYCM-58, native half in LYCM-72): books ingest
/// held back because a detector flagged them, waiting on a person to correct,
/// approve, or drop them.
///
/// Nothing here is a shelf concern — the server's shelf query already filters
/// pending books out — so this is its own controller rather than a view over
/// [LibraryController]. Approving one does move it onto the shelf, so callers
/// refresh the library after a queue action rather than trying to keep two
/// lists in step.
class ReviewController extends AsyncNotifier<List<Book>> {
  @override
  Future<List<Book>> build() async {
    if (!ref.watch(hasBackendProvider)) return const [];
    return ref.watch(lyceumClientProvider).listPendingReview();
  }

  Future<void> refresh() async {
    // No AsyncLoading here. Blanking the state swaps the whole queue for the
    // "Loading…" note, which on a pull-to-refresh reads as though everything
    // just left the queue — and the RefreshIndicator is already drawing the
    // spinner. The rows simply stay put until the new list lands.
    // (copyWithPrevious would say this more explicitly, but it is
    // package-internal in riverpod 3.3.2.)
    state = await AsyncValue.guard(
      () => ref.read(lyceumClientProvider).listPendingReview(),
    );
  }

  /// Publish a held book onto the shelf.
  ///
  /// The row leaves the queue optimistically and is put back at its old place if
  /// the server refuses — a failed approve that silently dropped the book from
  /// the queue would strand it pending with nothing left pointing at it.
  Future<void> approve(int bookId) =>
      _leaveQueue(bookId, (c) => c.approveBook(bookId));

  /// Drop a held book for good, along with its stored file (LYCM-109).
  Future<void> discard(int bookId) =>
      _leaveQueue(bookId, (c) => c.deleteBook(bookId));

  Future<void> _leaveQueue(
    int bookId,
    Future<void> Function(LyceumClient) act,
  ) async {
    final current = state.asData?.value;
    if (current == null) return;
    final index = current.indexWhere((b) => b.id == bookId);
    if (index == -1) return;
    final removed = current[index];

    state = AsyncData([...current]..removeAt(index));
    try {
      await act(ref.read(lyceumClientProvider));
    } catch (_) {
      final restored = [...state.asData?.value ?? const <Book>[]];
      restored.insert(index.clamp(0, restored.length), removed);
      state = AsyncData(restored);
      rethrow;
    }
  }

  /// Correct the title and author a converted file mangled. The row stays in the
  /// queue — an edit is not an approval.
  Future<void> saveMeta(int bookId, String title, String author) async {
    final updated = await ref
        .read(lyceumClientProvider)
        .updateBookMeta(bookId, title, author);
    _replace(updated);
  }

  /// Re-derive the cover from the external art source.
  Future<void> refetchCover(int bookId) async {
    final updated = await ref.read(lyceumClientProvider).refetchCover(bookId);
    _replace(updated);
  }

  /// Replace the cover with a chosen image. The server normalizes it, so the
  /// bytes served afterwards are not the bytes sent.
  Future<void> replaceCover(
    int bookId, {
    required String filename,
    String? path,
    List<int>? bytes,
  }) async {
    final updated = await ref
        .read(lyceumClientProvider)
        .replaceCover(bookId, filename: filename, path: path, bytes: bytes);
    _replace(updated);
  }

  void _replace(Book updated) {
    final current = state.asData?.value;
    if (current == null) return;
    state = AsyncData([
      for (final b in current)
        if (b.id == updated.id) updated else b,
    ]);
  }
}

final reviewControllerProvider =
    AsyncNotifierProvider<ReviewController, List<Book>>(ReviewController.new);

/// How many books are waiting, for the library's Review badge.
///
/// Zero while the queue is loading or unreachable: the badge is an invitation to
/// go and look, and inventing a count from an error would send someone to an
/// empty screen.
final pendingReviewCountProvider = Provider<int>(
  (ref) => ref.watch(reviewControllerProvider).asData?.value.length ?? 0,
);

/// The book a queued entry is a suspected copy of (LYCM-113).
///
/// Fetched per id rather than per row, so three copies of one book cost one
/// request. Null when that book has since been deleted — the flag outlives the
/// pointer by design, and the card says so instead of showing nothing.
final duplicateOfProvider = FutureProvider.autoDispose.family<Book?, int>(
  // No automatic retry. riverpod 3 retries a failed provider on a backoff by
  // default, which here means a 500 is quietly re-requested behind a panel that
  // has already told the reader to pull to refresh — two mechanisms racing at
  // the same job, one of them invisible. The refresh is the retry.
  retry: (_, _) => null,
  (ref, id) async {
    try {
      return await ref.watch(lyceumClientProvider).getBook(id);
    } on ApiException catch (e) {
      // 404 is the answer, not a fault: the matched book was deleted, and the
      // panel says so. Anything else — a 500, a timeout, a dropped
      // connection — must surface as an error instead, because the "deleted"
      // copy tells someone to approve a book the server is still holding as a
      // duplicate, at the moment the app knows least.
      if (e.isNotFound) return null;
      rethrow;
    }
  },
);
